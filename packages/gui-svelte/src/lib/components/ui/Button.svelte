<script lang="ts">
  import { cn } from "@/lib/utils/format";

  type Variant = "default" | "destructive" | "outline" | "secondary" | "ghost" | "link";
  type Size = "default" | "sm" | "lg" | "icon";

  export let variant: Variant = "default";
  export let size: Size = "default";
  export let disabled: boolean = false;
  export let type: "button" | "submit" | "reset" = "button";

  let className = "";
  export { className as class };

  const baseClasses =
    "inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50";

  const variantClasses: Record<Variant, string> = {
    default: "bg-primary text-primary-foreground hover:bg-primary/90",
    destructive: "bg-destructive text-destructive-foreground hover:bg-destructive/90",
    outline: "border border-input bg-background hover:bg-accent hover:text-accent-foreground",
    secondary: "bg-secondary text-secondary-foreground hover:bg-secondary/80",
    ghost: "hover:bg-accent hover:text-accent-foreground",
    link: "text-primary underline-offset-4 hover:underline",
  };

  const sizeClasses: Record<Size, string> = {
    default: "h-10 px-4 py-2",
    sm: "h-9 rounded-md px-3",
    lg: "h-11 rounded-md px-8",
    icon: "h-10 w-10",
  };

  $: classes = cn(baseClasses, variantClasses[variant], sizeClasses[size], className);
</script>

<button {type} {disabled} class={classes} {...$$restProps} on:click>
  <slot />
</button>
