<script lang="ts">
  import { cn } from "@/lib/utils/format";

  let className = "";
  export { className as class };

  $: classes = cn("w-full caption-bottom text-sm", className);
</script>

<div class="relative w-full overflow-auto">
  <table class={classes} {...$$restProps}>
    <slot />
  </table>
</div>
