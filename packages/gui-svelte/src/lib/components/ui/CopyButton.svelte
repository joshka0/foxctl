<script lang="ts">
  import { Copy, Check } from "lucide-svelte";
  import { Button } from "@/lib/components/ui";

  export let text: string;

  let copied = false;

  async function handleCopy() {
    await navigator.clipboard.writeText(text);
    copied = true;
    setTimeout(() => (copied = false), 2000);
  }
</script>

<Button variant="ghost" size="icon" on:click={handleCopy} class="h-8 w-8">
  {#if copied}
    <Check class="h-4 w-4 text-green-500" />
  {:else}
    <Copy class="h-4 w-4" />
  {/if}
</Button>
