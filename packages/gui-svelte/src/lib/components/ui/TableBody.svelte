<script lang="ts">
  import { cn } from "@/lib/utils/format";

  let className = "";
  export { className as class };

  $: classes = cn("[&_tr:last-child]:border-0", className);
</script>

<tbody class={classes} {...$$restProps}>
  <slot />
</tbody>
