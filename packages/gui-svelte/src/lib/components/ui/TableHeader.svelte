<script lang="ts">
  import { cn } from "@/lib/utils/format";

  let className = "";
  export { className as class };

  $: classes = cn("[&_tr]:border-b", className);
</script>

<thead class={classes} {...$$restProps}>
  <slot />
</thead>
