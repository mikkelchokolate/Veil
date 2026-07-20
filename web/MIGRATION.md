# S7 UI-primitive migration guide

The reusable component layer lives in `web/src/components/ui/`. Migrate pages
from raw HTML + custom CSS classes to these primitives WITHOUT changing
behavior, URLs, query keys, mutation payloads, or i18n keys.

## Import from
`../components/ui/<primitive>` (pages) or `./<primitive>` (within ui/).

## Available primitives (all in web/src/components/ui/)
- button.tsx: `Button` (variants: primary, default, outline, ghost, danger, success, link; sizes: default, sm, lg, icon). Default type="button".
- input.tsx: `Input`, `Textarea`
- select.tsx: `Select` (styled native <select>, keeps <option> children)
- label.tsx: `Label`
- dialog.tsx: `Dialog`, `DialogTrigger`, `DialogContent`, `DialogHeader`, `DialogFooter`, `DialogTitle`, `DialogDescription`, `DialogClose`
- alert-dialog.tsx: `AlertDialog`, `AlertDialogTrigger`, `AlertDialogContent`, `AlertDialogHeader`, `AlertDialogFooter`, `AlertDialogTitle`, `AlertDialogDescription`, `AlertDialogAction`, `AlertDialogCancel`
- tabs.tsx: `Tabs`, `TabsList`, `TabsTrigger`, `TabsContent`
- table.tsx: `Table`, `TableHeader`, `TableBody`, `TableRow`, `TableHead`, `TableCell`
- badge.tsx: `Badge` (variants: default, success, danger, warning, outline)
- dropdown-menu.tsx: `DropdownMenu`, `DropdownMenuTrigger`, `DropdownMenuContent`, `DropdownMenuItem`, `DropdownMenuCheckboxItem`, `DropdownMenuLabel`, `DropdownMenuSeparator`
- form.tsx: `FormItem`, `FormMessage`, `FormDescription`
- toast.tsx: `Toast`, `ToastProvider`, `ToastViewport`, `ToastTitle`, `ToastDescription`, `ToastClose`

## Mapping rules
- `<button className="btn">` → `<Button>`; `btn btn-primary` → `<Button variant="primary">`; `btn btn-danger` → `<Button variant="danger">`. Keep onClick/disabled/type. (Button defaults type="button"; pass type="submit" for form submits.)
- `<input className="input">` → `<Input>`; `<textarea className="input">` → `<Textarea>`. Keep id/value/onChange/placeholder/required/autoComplete/htmlFor wiring.
- `<select className="input">` → `<Select>` (keep <option> children).
- `<label htmlFor=...>` → `<Label htmlFor=...>`.
- `form-field` wrapper divs → `<FormItem>`; `form-error` → `<FormMessage>`; `muted` hint text → `<FormDescription>`.
- Status pill (`StatusBadge`, badge spans) → `<Badge variant=...>` (success=active/ok, danger=error/disabled, warning=pending, default otherwise). Keep the text.
- Custom modal (overlay + centered card, e.g. one-time credentials modal) → `Dialog` + `DialogContent` + `DialogHeader/Title/Description/Footer`. Open state: `<Dialog open={open} onOpenChange={setOpen}>`.
- Inline "Really delete ...? [Confirm] [Cancel]" confirmation → `AlertDialog` with `AlertDialogContent/Title/Description/Footer`, action = the destructive mutate, cancel closes. Trigger is the Delete button.
- Custom dropdown (toggle button + absolute card menu, e.g. column visibility) → `DropdownMenu` + `DropdownMenuTrigger` (a Button) + `DropdownMenuContent` + `DropdownMenuCheckboxItem` (checked + onCheckedChange).
- Tab strip (buttons switching active tab) → `Tabs` + `TabsList` + `TabsTrigger value=...` + `TabsContent value=...`.
- `<table>` markup → `Table/TableHeader/TableBody/TableRow/TableHead/TableCell`. TanStack Table rendering logic (flexRender, getHeaderGroups) stays identical; only the tags change.

## DO NOT
- Do not change any apiFetch/URL/queryKey/mutation body/navigate(to=).
- Do not change behavior, ordering, conditionals (isAdmin, confirmDelete, etc.).
- Do not remove accessibility attributes (id/htmlFor/aria-label/role); Dialog/AlertDialog manage focus — drop now-redundant manual close buttons only when DialogClose covers it.
- Do not touch src/api/generated/** or src/routeTree.gen.ts.

## Verify after each page
`cd web && pnpm run typecheck && node_modules/.bin/biome check --write --unsafe <file>`
Then `pnpm run test` and `pnpm run build` must stay green.
