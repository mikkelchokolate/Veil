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
	<div className="relative min-w-0 w-full max-w-full">
		<select
			ref={ref}
			className={cn(
				"flex h-9 w-full appearance-none items-center rounded-none border border-[var(--border)] bg-[var(--surface)] px-3 pr-10 py-1 text-sm text-[var(--fg)] shadow-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)] disabled:cursor-not-allowed disabled:opacity-50",
				className,
			)}
			{...props}
		>
			{children}
		</select>
		<ChevronDown
			className="pointer-events-none absolute right-3 top-1/2 h-4 w-4 -translate-y-1/2 opacity-60"
			aria-hidden="true"
		/>
	</div>
));
Select.displayName = "Select";

export { Select };
