import { cva, type VariantProps } from "class-variance-authority";
import type * as React from "react";
import { cn } from "../../lib/utils";

const badgeVariants = cva(
	"inline-flex items-center rounded-md border px-2 py-0.5 text-xs font-semibold transition-colors focus:outline-none",
	{
		variants: {
			variant: {
				default: "border-transparent bg-[var(--border)]/50 text-[var(--fg)]",
				success:
					"border-transparent bg-[var(--success)]/20 text-[var(--success)]",
				danger: "border-transparent bg-[var(--danger)]/20 text-[var(--danger)]",
				warning:
					"border-transparent bg-[var(--warning)]/20 text-[var(--warning)]",
				outline: "text-[var(--fg)] border-[var(--border)]",
			},
		},
		defaultVariants: { variant: "default" },
	},
);

export interface BadgeProps
	extends React.HTMLAttributes<HTMLSpanElement>,
		VariantProps<typeof badgeVariants> {}

function Badge({ className, variant, ...props }: BadgeProps) {
	return (
		<span className={cn(badgeVariants({ variant }), className)} {...props} />
	);
}

export { Badge, badgeVariants };
