<script lang="ts">
	import { ChevronRight, Copy, Check } from '@lucide/svelte';
	import { tryParseJson } from '$lib/utils/format';
	import { Button, IconButton } from '$lib/components/ui';

	interface Props {
		data: string | unknown;
		collapsed?: boolean;
		maxHeight?: string;
	}

	let { data, collapsed = true, maxHeight = '400px' }: Props = $props();

	// Initial state only, like before: the viewer then toggles on its own.
	// svelte-ignore state_referenced_locally
	let isCollapsed = $state(collapsed);
	let copySuccess = $state(false);

	let parsed = $derived(typeof data === 'string' ? tryParseJson(data) : data);
	let displayData = $derived(parsed !== null ? parsed : data);
	let isValidJson = $derived(parsed !== null);
	let formattedJson = $derived(isValidJson ? JSON.stringify(displayData, null, 2) : String(data));

	function toggleCollapse() {
		isCollapsed = !isCollapsed;
	}

	async function copyToClipboard() {
		try {
			await navigator.clipboard.writeText(formattedJson);
			copySuccess = true;
			setTimeout(() => (copySuccess = false), 2000);
		} catch (err) {
			console.error('Failed to copy:', err);
		}
	}

	function escapeHtml(s: string): string {
		return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
	}

	function syntaxHighlight(json: string): string {
		return escapeHtml(json).replace(
			/("(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false|null)\b|-?\d+(?:\.\d*)?(?:[eE][+\-]?\d+)?)/g,
			(match) => {
				let cls = 'json-number';
				if (/^"/.test(match)) {
					if (/:$/.test(match)) {
						cls = 'json-key';
					} else {
						cls = 'json-string';
					}
				} else if (/true|false/.test(match)) {
					cls = 'json-boolean';
				} else if (/null/.test(match)) {
					cls = 'json-null';
				}
				return `<span class="${cls}">${match}</span>`;
			}
		);
	}
</script>

<div class="json-viewer rounded-md border border-line-subtle bg-canvas font-mono text-xs">
	<div class="flex items-center justify-between gap-2 px-1.5 py-1 border-b border-line-subtle">
		<Button variant="ghost" size="sm" onclick={toggleCollapse} aria-expanded={!isCollapsed}>
			<ChevronRight
				size={14}
				strokeWidth={1.75}
				class="transition-transform duration-150 {isCollapsed ? '' : 'rotate-90'}"
			/>
			<span class="font-sans">{isValidJson ? 'JSON' : 'Raw'}</span>
			{#if isValidJson && typeof displayData === 'object' && displayData !== null}
				<span class="text-fg-subtle">
					{Array.isArray(displayData)
						? `[${displayData.length} items]`
						: `{${Object.keys(displayData).length} keys}`}
				</span>
			{/if}
		</Button>

		<IconButton
			icon={copySuccess ? Check : Copy}
			label={copySuccess ? 'Copied' : 'Copy to clipboard'}
			onclick={copyToClipboard}
		/>
	</div>

	{#if !isCollapsed}
		<div class="overflow-auto" style="max-height: {maxHeight}">
			<pre
				class="m-0 p-3 text-xs text-fg whitespace-pre-wrap break-all">{#if isValidJson}{@html syntaxHighlight(formattedJson)}{:else}{formattedJson}{/if}</pre>
		</div>
	{/if}
</div>
