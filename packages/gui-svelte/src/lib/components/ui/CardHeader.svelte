<script lang="ts">
  import { cn } from "@/lib/utils/format";

  let className = "";
  export { className as class };

  $: classes = cn("flex flex-col space-y-1.5 p-6", className);
</script>

<div class={classes} {...$$restProps}>
  <slot />
</div>
