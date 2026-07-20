import { ChevronDown } from "lucide-react";
import * as React from "react";
import { cn } from "../../lib/utils";

// Styled native <select>. The product has many simple single-select form
// fields; a styled native control preserves behavior (no portal/focus-trap
// surprises) while matching the design system. Use DropdownMenu for action
// menus and the Radix Select for rich option content.
const Select = React.forwardRef<
	HTMLSelectElement,
	React.SelectHTMLAttributes<HTMLSelectElement>
>(({ className, children, ...props }, ref) => (
	<div className="relative inline-flex w-full items-center">
		<select
			ref={ref}
			className={cn(
				"flex h-9 w-full appearance-none items-center rounded-md border border-[var(--border)] bg-[var(--surface)] px-3 pr-8 py-1 text-sm text-[var(--fg)] shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)] disabled:cursor-not-allowed disabled:opacity-50",
				className,
			)}
			{...props}
		>
			{children}
		</select>
		<ChevronDown
			className="pointer-events-none absolute right-2 h-4 w-4 opacity-60"
			aria-hidden="true"
		/>
	</div>
));
Select.displayName = "Select";

export { Select };
