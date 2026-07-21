import {
	keepPreviousData,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
	type ColumnDef,
	flexRender,
	getCoreRowModel,
	useReactTable,
	type VisibilityState,
} from "@tanstack/react-table";
import { useEffect, useMemo, useState } from "react";
import { listClients } from "../api/clients";
import { ApiError } from "../api/fetcher";
import { postApiV1ClientsBulk } from "../api/generated/clients/clients";
import type { ClientView } from "../api/generated/models";
import { useIsAdmin } from "../auth/AuthContext";
import {
	AlertDialog,
	AlertDialogAction,
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogFooter,
	AlertDialogHeader,
	AlertDialogTitle,
} from "../components/ui/alert-dialog";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import {
	DropdownMenu,
	DropdownMenuCheckboxItem,
	DropdownMenuContent,
	DropdownMenuTrigger,
} from "../components/ui/dropdown-menu";
import { FormMessage } from "../components/ui/form";
import { Input } from "../components/ui/input";
import { Select } from "../components/ui/select";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "../components/ui/table";
import { useI18n } from "../i18n/I18nContext";
import { fmtBytes } from "../lib/bytes";
import { Route } from "../routes/clients.index";

const STATUS_VARIANT: Record<
	string,
	"success" | "warning" | "danger" | "default"
> = {
	active: "success",
	disabled: "default",
	expired: "warning",
	depleted: "warning",
	pending_apply: "warning",
	apply_failed: "danger",
	orphaned: "danger",
};

function StatusBadge({ status }: { status: string }) {
	const { t } = useI18n();
	const variant = STATUS_VARIANT[status] ?? "default";
	const key = `clients.status.${status}`;
	const label = t(key) === key ? status : t(key);
	return <Badge variant={variant}>{label}</Badge>;
}

function fmtExpiry(ts?: number): string {
	if (!ts) return "—";
	return new Date(ts * 1000).toLocaleDateString();
}

const DEBOUNCE_MS = 300;

interface BulkResult {
	id: string;
	ok: boolean;
	message?: string;
}

export function ClientsPage() {
	const isAdmin = useIsAdmin();
	const { t } = useI18n();
	const navigate = useNavigate();
	const qc = useQueryClient();
	// Blocker W6: typed, route-validated search params. The Zod schema lives
	// in the file route (routes/clients.index.tsx) — the page never sees
	// unvalidated URL input. Absent params arrive undefined; defaults here.
	const parsed = Route.useSearch();

	const page = parsed.page ?? 1;
	const pageSize = parsed.pageSize ?? 25;
	const status = parsed.status ?? "";
	const inboundId = parsed.inboundId ?? "";
	const sort = parsed.sort ?? "created";
	const searchParam = parsed.search ?? "";

	// S3: debounced server-side search. The input is uncontrolled-local; the
	// debounced value is what actually reaches the query and the URL.
	const [searchInput, setSearchInput] = useState(searchParam);
	const [searchText, setSearchText] = useState(searchParam);
	useEffect(() => {
		const t = setTimeout(() => setSearchText(searchInput.trim()), DEBOUNCE_MS);
		return () => clearTimeout(t);
	}, [searchInput]);
	// Push the debounced value into the URL so it is shareable/restorable.
	useEffect(() => {
		if (searchText !== searchParam) {
			void navigate({
				to: "/clients",
				search: (prev) => ({
					...prev,
					search: searchText || undefined,
					page: 1,
				}),
				replace: true,
			});
		}
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [searchText, searchParam, navigate]);

	const query = useQuery({
		queryKey: [
			"clients",
			"list",
			{ page, pageSize, searchText, status, inboundId, sort },
		],
		queryFn: () =>
			listClients({
				page,
				pageSize,
				search: searchText,
				status,
				inboundId,
				sort,
			}),
		placeholderData: keepPreviousData,
	});

	const [selected, setSelected] = useState<Set<string>>(new Set());
	const [bulkError, setBulkError] = useState<string | null>(null);
	const [bulkResults, setBulkResults] = useState<BulkResult[] | null>(null);
	const [colVis, setColVis] = useState<VisibilityState>({});
	const [showColMenu, setShowColMenu] = useState(false);
	const [confirmDelete, setConfirmDelete] = useState(false);

	function setParam(patch: Record<string, string | undefined>) {
		void navigate({
			to: "/clients",
			search: (prev) => ({ ...prev, ...patch }),
			replace: true,
		});
	}

	const bulk = useMutation({
		mutationFn: (args: { action: string; ids: string[] }) =>
			postApiV1ClientsBulk({
				action: args.action as never,
				clientIds: args.ids,
			}),
		onSuccess: (data) => {
			setSelected(new Set());
			setBulkError(null);
			setConfirmDelete(false);
			// apiFetch returns the parsed body directly.
			const body = data as
				| {
						succeeded?: number;
						skipped?: number;
						failed?: number;
						results?: BulkResult[];
				  }
				| undefined;
			// S3: per-client bulk result, not just an aggregate.
			setBulkResults(body?.results ?? null);
			void qc.invalidateQueries({ queryKey: ["clients"] });
			void qc.invalidateQueries({ queryKey: ["apply"] });
		},
		onError: (err) => {
			setBulkResults(null);
			setBulkError(
				err instanceof ApiError ? err.message : t("clients.error.bulk"),
			);
		},
	});

	const items = (query.data?.items ?? []) as ClientView[];
	const total = query.data?.total ?? 0;
	const totalPages = Math.max(1, Math.ceil(total / pageSize));
	const pageCountLabel =
		total === 1
			? t("clients.pagination.count_one", { n: total })
			: t("clients.pagination.count_other", { n: total });
	const deleteCountLabel =
		selected.size === 1
			? t("clients.delete.count_one", { n: selected.size })
			: t("clients.delete.count_other", { n: selected.size });

	// S3: aggregate summary across the current page (bytes kept as numbers
	// server-side already; fmtBytes formats without precision loss). Per-client
	// traffic usage is a Traffic (S6) concern, not part of ClientView.
	const summary = useMemo(() => {
		let quota = 0;
		let active = 0;
		for (const c of items) {
			if (typeof c.quotaBytes === "number") quota += c.quotaBytes;
			if (c.status === "active") active++;
		}
		return { quota, active, count: items.length };
	}, [items]);

	const columns: ColumnDef<ClientView>[] = [
		...(isAdmin
			? [
					{
						id: "select",
						header: () => (
							<input
								type="checkbox"
								checked={items.length > 0 && selected.size === items.length}
								onChange={toggleAll}
								aria-label={t("clients.selectAll")}
							/>
						),
						cell: ({ row }) => (
							<input
								type="checkbox"
								checked={selected.has(row.original.id)}
								onChange={() => toggle(row.original.id)}
								aria-label={t("clients.selectRow", {
									name: row.original.name,
								})}
								onClick={(e) => e.stopPropagation()}
							/>
						),
						size: 32,
						enableHiding: false,
					} as ColumnDef<ClientView>,
				]
			: []),
		{
			accessorKey: "name",
			header: () => t("common.name"),
			cell: ({ row }) => (
				<div>
					<div>{row.original.name}</div>
					{row.original.email ? (
						<div className="muted" style={{ fontSize: 12 }}>
							{row.original.email}
						</div>
					) : null}
				</div>
			),
		},
		{
			accessorKey: "status",
			header: () => t("common.status"),
			cell: ({ row }) => <StatusBadge status={row.original.status} />,
		},
		{
			accessorKey: "inboundIds",
			header: () => t("clients.columns.inbounds"),
			cell: ({ row }) => (
				<span className="muted">{row.original.inboundIds?.length ?? 0}</span>
			),
		},
		{
			accessorKey: "quotaBytes",
			header: () => t("clients.columns.quota"),
			cell: ({ row }) => (
				<span className="muted">{fmtBytes(row.original.quotaBytes)}</span>
			),
		},
		{
			accessorKey: "expiresAt",
			header: () => t("clients.columns.expires"),
			cell: ({ row }) => (
				<span className="muted">{fmtExpiry(row.original.expiresAt)}</span>
			),
		},
	];

	const COLUMN_LABELS: Record<string, string> = {
		name: t("common.name"),
		status: t("common.status"),
		inboundIds: t("clients.columns.inbounds"),
		quotaBytes: t("clients.columns.quota"),
		expiresAt: t("clients.columns.expires"),
	};

	const table = useReactTable({
		data: items,
		columns,
		getCoreRowModel: getCoreRowModel(),
		state: { columnVisibility: colVis },
		onColumnVisibilityChange: setColVis,
	});

	function toggle(id: string) {
		setSelected((prev) => {
			const next = new Set(prev);
			if (next.has(id)) next.delete(id);
			else next.add(id);
			return next;
		});
	}

	function toggleAll() {
		if (selected.size === items.length) {
			setSelected(new Set());
		} else {
			setSelected(new Set(items.map((c) => c.id)));
		}
	}

	return (
		<>
			<div className="card">
				<div
					style={{
						display: "flex",
						gap: 12,
						alignItems: "center",
						flexWrap: "wrap",
					}}
				>
					<h2 style={{ margin: 0, flex: 1 }}>{t("clients.title")}</h2>
					<Input
						style={{ maxWidth: 260 }}
						placeholder={t("clients.searchPlaceholder")}
						value={searchInput}
						onChange={(e) => setSearchInput(e.target.value)}
						aria-label={t("clients.searchAriaLabel")}
					/>
					<Select
						style={{ maxWidth: 160 }}
						value={status}
						onChange={(e) =>
							setParam({ status: e.target.value || undefined, page: "1" })
						}
						aria-label={t("clients.filterStatusAriaLabel")}
					>
						<option value="">{t("clients.statusFilter.all")}</option>
						<option value="enabled">{t("clients.status.enabled")}</option>
						<option value="disabled">{t("clients.status.disabled")}</option>
						<option value="depleted">{t("clients.status.depleted")}</option>
					</Select>
					<Select
						style={{ maxWidth: 160 }}
						value={sort}
						onChange={(e) => setParam({ sort: e.target.value })}
						aria-label={t("clients.sortAriaLabel")}
					>
						<option value="created">{t("clients.sort.newest")}</option>
						<option value="name">{t("common.name")}</option>
						<option value="expires">{t("clients.sort.expiry")}</option>
					</Select>
					<DropdownMenu
						modal={false}
						open={showColMenu}
						onOpenChange={setShowColMenu}
					>
						<DropdownMenuTrigger asChild>
							<Button aria-label={t("clients.columnsToggleAriaLabel")}>
								{t("clients.columnsToggle")}
							</Button>
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end">
							{table
								.getAllLeafColumns()
								.filter((c) => c.getCanHide())
								.map((col) => (
									<DropdownMenuCheckboxItem
										key={col.id}
										checked={col.getIsVisible()}
										onCheckedChange={(v) => col.toggleVisibility(v === true)}
										onSelect={(e) => e.preventDefault()}
									>
										{COLUMN_LABELS[col.id] ?? col.id}
									</DropdownMenuCheckboxItem>
								))}
						</DropdownMenuContent>
					</DropdownMenu>
					{isAdmin ? (
						<Button
							variant="primary"
							onClick={() => void navigate({ to: "/clients/new" })}
						>
							{t("clients.newClient")}
						</Button>
					) : null}
				</div>
				{/* S3: aggregate summary for the current page */}
				{items.length > 0 ? (
					<div className="muted" style={{ marginTop: 8, fontSize: 13 }}>
						{t("clients.summary", {
							count: summary.count,
							active: summary.active,
						})}
						{summary.quota > 0
							? t("clients.summary.totalQuota", {
									quota: fmtBytes(summary.quota),
								})
							: ""}
					</div>
				) : null}
			</div>

			{isAdmin && selected.size > 0 ? (
				<div
					className="card"
					style={{
						display: "flex",
						gap: 8,
						alignItems: "center",
						flexWrap: "wrap",
					}}
				>
					<span className="muted">
						{t("clients.selected", { n: selected.size })}
					</span>
					<Button
						disabled={bulk.isPending}
						onClick={() =>
							bulk.mutate({ action: "enable", ids: [...selected] })
						}
					>
						{t("common.enable")}
					</Button>
					<Button
						disabled={bulk.isPending}
						onClick={() =>
							bulk.mutate({ action: "disable", ids: [...selected] })
						}
					>
						{t("common.disable")}
					</Button>
					<Button
						disabled={bulk.isPending}
						onClick={() =>
							bulk.mutate({ action: "reset_traffic", ids: [...selected] })
						}
					>
						{t("clients.resetTraffic")}
					</Button>
					<Button
						variant="danger"
						disabled={bulk.isPending}
						onClick={() => setConfirmDelete(true)}
					>
						{t("common.delete")}
					</Button>
					{bulkError ? <FormMessage>{bulkError}</FormMessage> : null}
				</div>
			) : null}

			{/* S3: per-client bulk result */}
			{bulkResults && bulkResults.length > 0 ? (
				<div className="card">
					<h2 style={{ fontSize: 14 }}>{t("clients.bulkResult.title")}</h2>
					{bulkResults.map((r) => (
						<div
							key={r.id}
							className="muted"
							style={{ fontSize: 13, display: "flex", gap: 8 }}
						>
							<span
								className={r.ok ? "badge badge-success" : "badge badge-danger"}
							>
								{r.ok
									? t("clients.bulkResult.ok")
									: t("clients.bulkResult.failed")}
							</span>
							<span className="mono" style={{ fontSize: 12 }}>
								{r.id}
							</span>
							{r.message ? <span>{r.message}</span> : null}
						</div>
					))}
				</div>
			) : null}

			<div className="card">
				{query.isLoading ? (
					<p className="muted">{t("common.loading")}</p>
				) : query.isError ? (
					<FormMessage>
						{query.error instanceof ApiError
							? query.error.message
							: t("clients.error.load")}
					</FormMessage>
				) : (
					<Table>
						<TableHeader>
							{table.getHeaderGroups().map((hg) => (
								<TableRow key={hg.id}>
									{hg.headers.map((h) => (
										<TableHead
											key={h.id}
											style={{
												width: h.getSize() !== 150 ? h.getSize() : undefined,
											}}
										>
											{flexRender(h.column.columnDef.header, h.getContext())}
										</TableHead>
									))}
								</TableRow>
							))}
						</TableHeader>
						<TableBody>
							{table.getRowModel().rows.length === 0 ? (
								<TableRow>
									<TableCell colSpan={columns.length} className="muted">
										{t("clients.empty")}
									</TableCell>
								</TableRow>
							) : (
								table.getRowModel().rows.map((row) => (
									<TableRow
										key={row.id}
										style={{ cursor: "pointer" }}
										onClick={() =>
											void navigate({ to: `/clients/${row.original.id}` })
										}
									>
										{row.getVisibleCells().map((cell) => (
											<TableCell key={cell.id}>
												{flexRender(
													cell.column.columnDef.cell,
													cell.getContext(),
												)}
											</TableCell>
										))}
									</TableRow>
								))
							)}
						</TableBody>
					</Table>
				)}

				<div
					style={{
						display: "flex",
						gap: 12,
						alignItems: "center",
						marginTop: 16,
					}}
				>
					<span className="muted">
						{pageCountLabel} ·{" "}
						{t("clients.pagination.pageInfo", { page, totalPages })}
					</span>
					<div style={{ flex: 1 }} />
					<Button
						disabled={page <= 1}
						onClick={() => setParam({ page: String(page - 1) })}
					>
						{t("clients.pagination.previous")}
					</Button>
					<Button
						disabled={page >= totalPages}
						onClick={() => setParam({ page: String(page + 1) })}
					>
						{t("common.next")}
					</Button>
				</div>
			</div>

			<AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
				<AlertDialogContent>
					<AlertDialogHeader>
						<AlertDialogTitle>{t("clients.delete.title")}</AlertDialogTitle>
						<AlertDialogDescription>
							{t("clients.delete.confirmation", { count: deleteCountLabel })}
						</AlertDialogDescription>
					</AlertDialogHeader>
					<AlertDialogFooter>
						<AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
						<AlertDialogAction
							disabled={bulk.isPending}
							onClick={(e) => {
								e.preventDefault();
								bulk.mutate({ action: "delete", ids: [...selected] });
							}}
						>
							{bulk.isPending
								? t("clients.delete.confirmPending")
								: t("clients.delete.confirm")}
						</AlertDialogAction>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>
		</>
	);
}
