<script lang="ts">
  import { cn } from "@/lib/utils/format";
  import { ChevronDown } from "lucide-svelte";

  export let value: string = "";
  export let disabled: boolean = false;

  let className = "";
  export { className as class };

  $: classes = cn(
    "flex h-10 w-full appearance-none rounded-md border border-input bg-background px-3 py-2 pr-8 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50",
    className
  );
</script>

<div class="relative">
  <select bind:value {disabled} class={classes} {...$$restProps} on:change>
    <slot />
  </select>
  <ChevronDown class="absolute right-2.5 top-2.5 h-5 w-5 opacity-50 pointer-events-none" />
</div>
