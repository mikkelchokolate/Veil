import * as React from "react";
import { cn } from "../../lib/utils";

// Form field primitives. These complement react-hook-form: FormField wraps
// Label + control + error message with consistent spacing and accessible
// wiring (aria-describedby, aria-invalid).

const FormItem = React.forwardRef<
	HTMLDivElement,
	React.HTMLAttributes<HTMLDivElement>
>(({ className, ...props }, ref) => (
	<div
		ref={ref}
		className={cn("flex flex-col gap-1.5", className)}
		{...props}
	/>
));
FormItem.displayName = "FormItem";

const FormMessage = React.forwardRef<
	HTMLParagraphElement,
	React.HTMLAttributes<HTMLParagraphElement>
>(({ className, children, ...props }, ref) => {
	if (!children) return null;
	return (
		<p
			ref={ref}
			role="alert"
			className={cn("text-sm font-medium text-[var(--danger)]", className)}
			{...props}
		>
			{children}
		</p>
	);
});
FormMessage.displayName = "FormMessage";

const FormDescription = React.forwardRef<
	HTMLParagraphElement,
	React.HTMLAttributes<HTMLParagraphElement>
>(({ className, ...props }, ref) => (
	<p
		ref={ref}
		className={cn("text-xs text-[var(--muted)]", className)}
		{...props}
	/>
));
FormDescription.displayName = "FormDescription";

export { FormDescription, FormItem, FormMessage };
