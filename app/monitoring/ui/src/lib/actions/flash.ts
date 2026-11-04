// use:flash={value} — briefly highlights an element when `value` changes after the first
// render (auto-refresh updates). The animation is cut by prefers-reduced-motion (app.css).
export function flash(node: HTMLElement, value: unknown) {
	let previous = value;
	function onEnd() {
		node.classList.remove('rq-flash');
	}
	node.addEventListener('animationend', onEnd);
	return {
		update(next: unknown) {
			if (next === previous) return;
			previous = next;
			node.classList.remove('rq-flash');
			// Restart the animation when values change in quick succession.
			void node.offsetWidth;
			node.classList.add('rq-flash');
		},
		destroy() {
			node.removeEventListener('animationend', onEnd);
		}
	};
}
