<script lang="ts">
  import { cn } from "@/lib/utils/format";

  let className = "";
  export { className as class };

  $: classes = cn("flex items-center p-6 pt-0", className);
</script>

<div class={classes} {...$$restProps}>
  <slot />
</div>
