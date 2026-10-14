package alerts

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"time"

	queueworker "github.com/Publikey/runqy/queues"
)

// Email is one notification message.
type Email struct {
	From    string   // RFC 5322 address, e.g. "runqy <alerts@example.com>"
	To      []string // recipient addresses
	Subject string
	Text    string
	HTML    string
}

// Sender delivers an Email.
type Sender interface {
	Send(ctx context.Context, m Email) error
}

// sendTimeout bounds one delivery attempt.
const sendTimeout = 30 * time.Second

// NewSender builds the sender configured in settings, resolving its secret.
func NewSender(ctx context.Context, s *queueworker.AlertSettings, vault VaultReader) (Sender, error) {
	switch s.Provider {
	case queueworker.AlertProviderSMTP:
		if s.SMTP == nil || s.SMTP.Host == "" {
			return nil, fmt.Errorf("smtp.host is not configured")
		}
		password, err := ResolveSecret(ctx, s.SMTP.Password, EnvSMTPPassword, vault, os.Getenv)
		if err != nil {
			return nil, err
		}
		return &smtpSender{cfg: *s.SMTP, password: password}, nil
	case queueworker.AlertProviderSendGrid:
		var key string
		var err error
		if s.SendGrid != nil {
			key, err = ResolveSecret(ctx, s.SendGrid.APIKey, EnvSendGridAPIKey, vault, os.Getenv)
		} else {
			key = os.Getenv(EnvSendGridAPIKey)
		}
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, fmt.Errorf("no SendGrid API key: set sendgrid.api_key or %s", EnvSendGridAPIKey)
		}
		return &sendGridSender{apiKey: key, baseURL: "https://api.sendgrid.com", client: &http.Client{Timeout: sendTimeout}}, nil
	default:
		return nil, fmt.Errorf("unknown alert provider %q", s.Provider)
	}
}

// ---- SMTP ----

type smtpSender struct {
	cfg      queueworker.SMTPSettings
	password string
}

func (s *smtpSender) port() int {
	if s.cfg.Port > 0 {
		return s.cfg.Port
	}
	switch s.cfg.TLS {
	case "tls":
		return 465
	case "none":
		return 25
	default:
		return 587
	}
}

func (s *smtpSender) Send(ctx context.Context, m Email) error {
	from, err := mail.ParseAddress(m.From)
	if err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	msg, err := buildMIME(m)
	if err != nil {
		return err
	}

	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.port()))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	deadline := time.Now().Add(sendTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	tlsCfg := &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}

	var conn net.Conn
	if s.cfg.TLS == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp connect %s: %w", addr, err)
	}
	_ = conn.SetDeadline(deadline)

	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp handshake: %w", err)
	}
	defer c.Close()

	if s.cfg.TLS == "" || s.cfg.TLS == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("smtp server %s does not offer STARTTLS (set smtp.tls to \"tls\" or \"none\")", addr)
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if s.cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.cfg.Username, s.password, s.cfg.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	for _, to := range m.To {
		addr, err := mail.ParseAddress(to)
		if err != nil {
			return fmt.Errorf("invalid recipient %q: %w", to, err)
		}
		if err := c.Rcpt(addr.Address); err != nil {
			return fmt.Errorf("smtp RCPT TO %s: %w", addr.Address, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp DATA end: %w", err)
	}
	return c.Quit()
}

// buildMIME renders a multipart/alternative (text + HTML) message.
func buildMIME(m Email) ([]byte, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	headers := []string{
		"From: " + m.From,
		"To: " + strings.Join(m.To, ", "),
		"Subject: " + mime.QEncoding.Encode("utf-8", m.Subject),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"Message-ID: " + messageID(m.From),
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative; boundary=" + mw.Boundary(),
	}
	var out bytes.Buffer
	out.WriteString(strings.Join(headers, "\r\n") + "\r\n\r\n")

	for _, part := range []struct{ ctype, body string }{{"text/plain", m.Text}, {"text/html", m.HTML}} {
		if part.body == "" {
			continue
		}
		pw, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.ctype + "; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, err
		}
		qp := quotedprintable.NewWriter(pw)
		if _, err := io.WriteString(qp, part.body); err != nil {
			return nil, err
		}
		if err := qp.Close(); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	out.Write(buf.Bytes())
	return out.Bytes(), nil
}

func messageID(from string) string {
	domain := "runqy.local"
	if a, err := mail.ParseAddress(from); err == nil {
		if i := strings.LastIndex(a.Address, "@"); i >= 0 {
			domain = a.Address[i+1:]
		}
	}
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "<" + hex.EncodeToString(b) + "@" + domain + ">"
}

// ---- SendGrid ----

type sendGridSender struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

type sgAddress struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type sgContent struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type sgPersonalization struct {
	To []sgAddress `json:"to"`
}

type sgMail struct {
	Personalizations []sgPersonalization `json:"personalizations"`
	From             sgAddress           `json:"from"`
	Subject          string              `json:"subject"`
	Content          []sgContent         `json:"content"`
}

func (s *sendGridSender) Send(ctx context.Context, m Email) error {
	from, err := mail.ParseAddress(m.From)
	if err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	body := sgMail{From: sgAddress{Email: from.Address, Name: from.Name}, Subject: m.Subject}
	var to []sgAddress
	for _, r := range m.To {
		a, err := mail.ParseAddress(r)
		if err != nil {
			return fmt.Errorf("invalid recipient %q: %w", r, err)
		}
		to = append(to, sgAddress{Email: a.Address, Name: a.Name})
	}
	body.Personalizations = []sgPersonalization{{To: to}}
	// SendGrid requires text/plain before text/html.
	if m.Text != "" {
		body.Content = append(body.Content, sgContent{Type: "text/plain", Value: m.Text})
	}
	if m.HTML != "" {
		body.Content = append(body.Content, sgContent{Type: "text/html", Value: m.HTML})
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/v3/mail/send", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sendgrid: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("sendgrid: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	return nil
}
