<script lang="ts">
  import { cn } from "@/lib/utils/format";

  export let value: string = "";
  export let placeholder: string = "";
  export let disabled: boolean = false;

  let className = "";
  export { className as class };

  $: classes = cn(
    "flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50",
    className
  );
</script>

<input
  type="text"
  {placeholder}
  {disabled}
  bind:value
  class={classes}
  {...$$restProps}
  on:input
  on:change
  on:focus
  on:blur
/>
