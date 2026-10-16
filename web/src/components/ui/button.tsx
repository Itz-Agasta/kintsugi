import { Button as ButtonPrimitive } from "@base-ui/react/button";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "cn";

const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center gap-2 rounded-md border font-sans font-medium whitespace-nowrap select-none transition-colors duration-150 ease-out disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        default: "border-line bg-panel text-text hover:bg-raised",
        ghost: "border-transparent bg-transparent text-muted hover:bg-raised hover:text-text",
        primary: "border-text bg-text text-bg hover:bg-muted hover:border-muted",
        repair: "border-gold-line bg-gold-tint text-gold disabled:opacity-100",
      },
      size: {
        default: "h-8 px-3 text-[13px]",
        sm: "h-7 px-2.5 text-[12.5px]",
        lg: "h-10 rounded-[7px] px-4 text-sm",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);

function Button({
  className,
  variant = "default",
  size = "default",
  ...props
}: ButtonPrimitive.Props & VariantProps<typeof buttonVariants>) {
  return (
    <ButtonPrimitive
      data-slot="button"
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  );
}

export { Button };
