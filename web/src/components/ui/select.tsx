import * as React from "react";
import { cn } from "../../lib/utils";

// Styled native <select>. Width/maxWidth belong on the wrapper so the CSS
// chevron stays on the control; appearance and radius are owned by the theme.
const Select = React.forwardRef<
	HTMLSelectElement,
	React.SelectHTMLAttributes<HTMLSelectElement>
>(({ className, style, children, ...props }, ref) => (
	<div className="relative min-w-0 w-full max-w-full" style={style}>
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
	</div>
));
Select.displayName = "Select";

export { Select };
