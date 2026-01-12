<script lang="ts">
  import { cn } from "@/lib/utils/format";

  let className = "";
  export { className as class };

  $: classes = cn("text-2xl font-semibold leading-none tracking-tight", className);
</script>

<h3 class={classes} {...$$restProps}>
  <slot />
</h3>
