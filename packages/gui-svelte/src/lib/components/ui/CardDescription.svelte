<script lang="ts">
  import { cn } from "@/lib/utils/format";

  let className = "";
  export { className as class };

  $: classes = cn("text-sm text-muted-foreground", className);
</script>

<p class={classes} {...$$restProps}>
  <slot />
</p>
