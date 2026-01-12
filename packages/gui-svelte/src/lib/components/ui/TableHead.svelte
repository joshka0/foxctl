<script lang="ts">
  import { cn } from "@/lib/utils/format";

  let className = "";
  export { className as class };

  $: classes = cn(
    "h-12 px-4 text-left align-middle font-medium text-muted-foreground [&:has([role=checkbox])]:pr-0",
    className
  );
</script>

<th class={classes} {...$$restProps}>
  <slot />
</th>
