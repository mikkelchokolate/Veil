import { cva, type VariantProps } from "class-variance-authority";
import * as React from "react";
import { cn } from "../../lib/utils";

const buttonVariants = cva(
	"inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-offset-1 disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0",
	{
		variants: {
			variant: {
				primary:
					"bg-[var(--primary)] text-[#0a1414] hover:brightness-95 font-semibold",
				default:
					"bg-[var(--surface)] text-[var(--fg)] border border-[var(--border)] hover:bg-[var(--border)]/40",
				outline:
					"border border-[var(--border)] bg-transparent hover:bg-[var(--surface)] text-[var(--fg)]",
				ghost: "hover:bg-[var(--surface)] text-[var(--fg)]",
				danger:
					"bg-[var(--danger)] text-white hover:brightness-110 font-semibold",
				success: "bg-[var(--success)] text-white hover:brightness-110",
				link: "text-[var(--accent)] underline-offset-4 hover:underline",
			},
			size: {
				default: "h-9 px-4 py-2",
				sm: "h-8 rounded-md px-3 text-xs",
				lg: "h-10 rounded-md px-6",
				icon: "h-9 w-9",
			},
		},
		defaultVariants: { variant: "default", size: "default" },
	},
);

export interface ButtonProps
	extends React.ButtonHTMLAttributes<HTMLButtonElement>,
		VariantProps<typeof buttonVariants> {}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
	({ className, variant, size, type = "button", ...props }, ref) => (
		<button
			ref={ref}
			type={type}
			className={cn(buttonVariants({ variant, size }), className)}
			{...props}
		/>
	),
);
Button.displayName = "Button";

export { Button, buttonVariants };
