<script lang="ts">
  import { cn } from "@/lib/utils/format";

  type Variant =
    | "default"
    | "secondary"
    | "destructive"
    | "outline"
    | "success"
    | "warning"
    | "info"
    | "muted";

  export let variant: Variant = "default";

  let className = "";
  export { className as class };

  const baseClasses =
    "inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2";

  const variantClasses: Record<Variant, string> = {
    default: "border-transparent bg-primary text-primary-foreground",
    secondary: "border-transparent bg-secondary text-secondary-foreground",
    destructive: "border-transparent bg-destructive text-destructive-foreground",
    outline: "text-foreground",
    success: "border-transparent bg-green-500 text-white",
    warning: "border-transparent bg-yellow-500 text-white",
    info: "border-transparent bg-blue-500 text-white",
    muted: "border-transparent bg-muted text-muted-foreground",
  };

  $: classes = cn(baseClasses, variantClasses[variant], className);
</script>

<div class={classes} {...$$restProps}>
  <slot />
</div>
