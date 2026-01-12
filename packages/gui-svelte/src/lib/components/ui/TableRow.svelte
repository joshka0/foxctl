<script lang="ts">
  import { cn } from "@/lib/utils/format";

  let className = "";
  export { className as class };

  $: classes = cn(
    "border-b transition-colors hover:bg-muted/50 data-[state=selected]:bg-muted",
    className
  );
</script>

<tr class={classes} {...$$restProps}>
  <slot />
</tr>
