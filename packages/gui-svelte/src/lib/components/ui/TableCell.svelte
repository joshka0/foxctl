<script lang="ts">
  import { cn } from "@/lib/utils/format";

  let className = "";
  export { className as class };

  $: classes = cn("p-4 align-middle [&:has([role=checkbox])]:pr-0", className);
</script>

<td class={classes} {...$$restProps}>
  <slot />
</td>
