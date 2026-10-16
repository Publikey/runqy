package alerts

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	queueworker "github.com/Publikey/runqy/queues"
)

// fakeSMTP is a minimal SMTP server recording one session.
type fakeSMTP struct {
	ln       net.Listener
	starttls bool
	mu       sync.Mutex
	cmds     []string
	data     string
}

func startFakeSMTP(t *testing.T, starttls bool) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSMTP{ln: ln, starttls: starttls}
	t.Cleanup(func() { ln.Close() })
	go s.serve()
	return s
}

func (s *fakeSMTP) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *fakeSMTP) serve() {
	conn, err := s.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := func(line string) { conn.Write([]byte(line + "\r\n")) }
	w("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		s.mu.Lock()
		s.cmds = append(s.cmds, line)
		s.mu.Unlock()
		switch cmd := strings.ToUpper(strings.SplitN(line, " ", 2)[0]); cmd {
		case "EHLO", "HELO":
			if s.starttls {
				w("250-fake")
				w("250 STARTTLS")
			} else {
				w("250 fake")
			}
		case "MAIL", "RCPT":
			w("250 OK")
		case "DATA":
			w("354 go ahead")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			s.mu.Lock()
			s.data = b.String()
			s.mu.Unlock()
			w("250 queued")
		case "QUIT":
			w("221 bye")
			return
		default:
			w("502 not implemented")
		}
	}
}

func TestSMTPSenderPlain(t *testing.T) {
	srv := startFakeSMTP(t, false)
	sender := &smtpSender{cfg: queueworker.SMTPSettings{Host: "127.0.0.1", Port: srv.port(), TLS: "none"}}
	err := sender.Send(context.Background(), Email{
		From: "runqy <alerts@example.com>", To: []string{"ops@example.com", "Bob <bob@example.com>"},
		Subject: "[runqy] FIRING é", Text: "plain body", HTML: "<p>html body</p>",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	joined := strings.Join(srv.cmds, "\n")
	for _, want := range []string{"MAIL FROM:<alerts@example.com>", "RCPT TO:<ops@example.com>", "RCPT TO:<bob@example.com>"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in session:\n%s", want, joined)
		}
	}
	for _, want := range []string{"multipart/alternative", "plain body", "<p>html body</p>", "Subject: =?utf-8?q?"} {
		if !strings.Contains(srv.data, want) {
			t.Errorf("message lacks %q:\n%s", want, srv.data)
		}
	}
}

func TestSMTPSenderRequiresStartTLSByDefault(t *testing.T) {
	srv := startFakeSMTP(t, false)
	sender := &smtpSender{cfg: queueworker.SMTPSettings{Host: "127.0.0.1", Port: srv.port()}} // tls defaults to starttls
	err := sender.Send(context.Background(), Email{From: "a@example.com", To: []string{"b@example.com"}, Subject: "s", Text: "t"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("want a STARTTLS error (never send credentials in clear by default), got %v", err)
	}
}

func TestSMTPDefaultPorts(t *testing.T) {
	for tlsMode, want := range map[string]int{"": 587, "starttls": 587, "tls": 465, "none": 25} {
		if got := (&smtpSender{cfg: queueworker.SMTPSettings{TLS: tlsMode}}).port(); got != want {
			t.Errorf("tls %q: port %d, want %d", tlsMode, got, want)
		}
	}
}

func TestSendGridSender(t *testing.T) {
	var got sgMail
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/mail/send" {
			http.NotFound(w, r)
			return
		}
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	s := &sendGridSender{apiKey: "SG.key", baseURL: srv.URL, client: srv.Client()}
	err := s.Send(context.Background(), Email{From: "runqy <a@example.com>", To: []string{"ops@example.com"}, Subject: "subj", Text: "t", HTML: "<b>h</b>"})
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer SG.key" {
		t.Errorf("auth header = %q", auth)
	}
	if got.From.Email != "a@example.com" || got.From.Name != "runqy" || got.Subject != "subj" ||
		len(got.Personalizations) != 1 || got.Personalizations[0].To[0].Email != "ops@example.com" ||
		len(got.Content) != 2 || got.Content[0].Type != "text/plain" {
		t.Errorf("unexpected payload: %+v", got)
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errors":[{"message":"bad key"}]}`, http.StatusUnauthorized)
	}))
	defer failing.Close()
	s = &sendGridSender{apiKey: "x", baseURL: failing.URL, client: failing.Client()}
	if err := s.Send(context.Background(), Email{From: "a@example.com", To: []string{"b@example.com"}}); err == nil ||
		!strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "bad key") {
		t.Errorf("want a 401 error with SendGrid's message, got %v", err)
	}
}

func TestNewSenderResolvesSecrets(t *testing.T) {
	t.Setenv(EnvSendGridAPIKey, "SG.from-env")
	s, err := NewSender(context.Background(), &queueworker.AlertSettings{Provider: queueworker.AlertProviderSendGrid}, nil)
	if err != nil || s.(*sendGridSender).apiKey != "SG.from-env" {
		t.Fatalf("env fallback: %v %+v", err, s)
	}
	t.Setenv(EnvSendGridAPIKey, "")
	if _, err := NewSender(context.Background(), &queueworker.AlertSettings{Provider: queueworker.AlertProviderSendGrid}, nil); err == nil {
		t.Fatal("want an error without any SendGrid key")
	}

	vault := fakeVault{"mail/smtp_password": "s3cret"}
	s, err = NewSender(context.Background(), &queueworker.AlertSettings{
		Provider: queueworker.AlertProviderSMTP,
		SMTP:     &queueworker.SMTPSettings{Host: "smtp.example.com", Password: "vault://mail/smtp_password"},
	}, vault)
	if err != nil || s.(*smtpSender).password != "s3cret" {
		t.Fatalf("vault reference: %v", err)
	}
}

type fakeVault map[string]string

func (f fakeVault) GetEntry(_ context.Context, vault, key string) (string, bool, error) {
	if v, ok := f[vault+"/"+key]; ok {
		return v, true, nil
	}
	return "", false, &net.AddrError{Err: "not found", Addr: vault + "/" + key}
}

func TestResolveSecret(t *testing.T) {
	env := func(k string) string { return map[string]string{"X": "from-env"}[k] }
	vault := fakeVault{"v/k": "from-vault"}
	cases := []struct{ in, want string }{{"", "from-env"}, {"literal", "literal"}, {"vault://v/k", "from-vault"}}
	for _, c := range cases {
		got, err := ResolveSecret(context.Background(), c.in, "X", vault, env)
		if err != nil || got != c.want {
			t.Errorf("ResolveSecret(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{"vault://nokey", "vault:///k", "vault://v/missing"} {
		if _, err := ResolveSecret(context.Background(), bad, "X", vault, env); err == nil {
			t.Errorf("ResolveSecret(%q): want error", bad)
		}
	}
	if _, err := ResolveSecret(context.Background(), "vault://v/k", "X", nil, env); err == nil {
		t.Error("vault reference without vaults: want error")
	}
}
