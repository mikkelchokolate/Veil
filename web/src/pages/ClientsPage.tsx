import {
	keepPreviousData,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import {
	type ColumnDef,
	flexRender,
	getCoreRowModel,
	useReactTable,
	type VisibilityState,
} from "@tanstack/react-table";
import { useEffect, useMemo, useState } from "react";
import { z } from "zod";
import { listClients } from "../api/clients";
import { ApiError } from "../api/fetcher";
import { postApiV1ClientsBulk } from "../api/generated/clients/clients";
import type { ClientView } from "../api/generated/models";
import { useIsAdmin } from "../auth/AuthContext";
import { fmtBytes } from "../lib/bytes";

const STATUS_BADGE: Record<string, { label: string; cls: string }> = {
	active: { label: "active", cls: "badge-success" },
	disabled: { label: "disabled", cls: "" },
	expired: { label: "expired", cls: "badge-warning" },
	depleted: { label: "depleted", cls: "badge-warning" },
	pending_apply: { label: "pending apply", cls: "badge-warning" },
	apply_failed: { label: "apply failed", cls: "badge-danger" },
	orphaned: { label: "orphaned", cls: "badge-danger" },
};

function StatusBadge({ status }: { status: string }) {
	const meta = STATUS_BADGE[status] ?? { label: status, cls: "" };
	return <span className={`badge ${meta.cls}`}>{meta.label}</span>;
}

function fmtExpiry(ts?: number): string {
	if (!ts) return "—";
	return new Date(ts * 1000).toLocaleDateString();
}

/** S3: search params validated with Zod — every value the URL carries is
 * parsed/coerced/defaulted here so a hand-edited or stale URL can never put
 * the page in an invalid state. */
const searchSchema = z.object({
	page: z.coerce.number().int().positive().catch(1),
	pageSize: z.coerce.number().int().positive().max(200).catch(25),
	search: z.string().catch(""),
	status: z.string().catch(""),
	inboundId: z.string().catch(""),
	sort: z.string().catch("created"),
});

const DEBOUNCE_MS = 300;

interface BulkResult {
	id: string;
	ok: boolean;
	message?: string;
}

export function ClientsPage() {
	const isAdmin = useIsAdmin();
	const navigate = useNavigate();
	const qc = useQueryClient();
	const rawSearch = useSearch({ strict: false }) as Record<string, unknown>;
	const parsed = searchSchema.parse(rawSearch);

	const page = parsed.page;
	const pageSize = parsed.pageSize;
	const status = parsed.status;
	const inboundId = parsed.inboundId;
	const sort = parsed.sort;

	// S3: debounced server-side search. The input is uncontrolled-local; the
	// debounced value is what actually reaches the query and the URL.
	const [searchInput, setSearchInput] = useState(parsed.search);
	const [searchText, setSearchText] = useState(parsed.search);
	useEffect(() => {
		const t = setTimeout(() => setSearchText(searchInput.trim()), DEBOUNCE_MS);
		return () => clearTimeout(t);
	}, [searchInput]);
	// Push the debounced value into the URL so it is shareable/restorable.
	useEffect(() => {
		if (searchText !== parsed.search) {
			void navigate({
				to: "/clients",
				search: (prev: Record<string, unknown>) => ({
					...prev,
					search: searchText || undefined,
					page: 1,
				}),
				replace: true,
			});
		}
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [searchText, parsed.search, navigate]);

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
			search: (prev: Record<string, unknown>) => ({ ...prev, ...patch }),
			replace: true,
		});
	}

	const bulk = useMutation({
		mutationFn: (args: { action: string; ids: string[] }) =>
			postApiV1ClientsBulk({
				action: args.action as never,
				clientIds: args.ids,
			}),
		onSuccess: (res) => {
			setSelected(new Set());
			setBulkError(null);
			setConfirmDelete(false);
			const data = res.data as
				| {
						succeeded?: number;
						skipped?: number;
						failed?: number;
						results?: BulkResult[];
				  }
				| undefined;
			// S3: per-client bulk result, not just an aggregate.
			setBulkResults(data?.results ?? null);
			void qc.invalidateQueries({ queryKey: ["clients"] });
			void qc.invalidateQueries({ queryKey: ["apply"] });
		},
		onError: (err) => {
			setBulkResults(null);
			setBulkError(
				err instanceof ApiError ? err.message : "Bulk action failed",
			);
		},
	});

	const items = (query.data?.items ?? []) as ClientView[];
	const total = query.data?.total ?? 0;
	const totalPages = Math.max(1, Math.ceil(total / pageSize));

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
								aria-label="select all"
							/>
						),
						cell: ({ row }) => (
							<input
								type="checkbox"
								checked={selected.has(row.original.id)}
								onChange={() => toggle(row.original.id)}
								aria-label={`select ${row.original.name}`}
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
			header: "Name",
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
			header: "Status",
			cell: ({ row }) => <StatusBadge status={row.original.status} />,
		},
		{
			accessorKey: "inboundIds",
			header: "Inbounds",
			cell: ({ row }) => (
				<span className="muted">{row.original.inboundIds?.length ?? 0}</span>
			),
		},
		{
			accessorKey: "quotaBytes",
			header: "Quota",
			cell: ({ row }) => (
				<span className="muted">{fmtBytes(row.original.quotaBytes)}</span>
			),
		},
		{
			accessorKey: "expiresAt",
			header: "Expires",
			cell: ({ row }) => (
				<span className="muted">{fmtExpiry(row.original.expiresAt)}</span>
			),
		},
	];

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
					<h2 style={{ margin: 0, flex: 1 }}>Clients</h2>
					<input
						className="input"
						style={{ maxWidth: 260 }}
						placeholder="Search name or email"
						value={searchInput}
						onChange={(e) => setSearchInput(e.target.value)}
						aria-label="search clients"
					/>
					<select
						className="input"
						style={{ maxWidth: 160 }}
						value={status}
						onChange={(e) =>
							setParam({ status: e.target.value || undefined, page: "1" })
						}
						aria-label="filter status"
					>
						<option value="">All statuses</option>
						<option value="enabled">Enabled</option>
						<option value="disabled">Disabled</option>
						<option value="depleted">Depleted</option>
					</select>
					<select
						className="input"
						style={{ maxWidth: 160 }}
						value={sort}
						onChange={(e) => setParam({ sort: e.target.value })}
						aria-label="sort"
					>
						<option value="created">Newest</option>
						<option value="name">Name</option>
						<option value="expires">Expiry</option>
					</select>
					<div style={{ position: "relative" }}>
						<button
							type="button"
							className="btn"
							onClick={() => setShowColMenu((v) => !v)}
							aria-label="toggle columns"
						>
							Columns
						</button>
						{showColMenu ? (
							<div
								className="card"
								style={{
									position: "absolute",
									right: 0,
									top: "110%",
									zIndex: 20,
									padding: 8,
									minWidth: 160,
								}}
							>
								{table
									.getAllLeafColumns()
									.filter((c) => c.getCanHide())
									.map((col) => (
										<label
											key={col.id}
											style={{
												display: "flex",
												gap: 8,
												alignItems: "center",
												padding: "4px 2px",
												cursor: "pointer",
											}}
										>
											<input
												type="checkbox"
												checked={col.getIsVisible()}
												onChange={col.getToggleVisibilityHandler()}
											/>
											<span style={{ fontSize: 13 }}>{col.id}</span>
										</label>
									))}
							</div>
						) : null}
					</div>
					{isAdmin ? (
						<button
							type="button"
							className="btn btn-primary"
							onClick={() => void navigate({ to: "/clients/new" })}
						>
							New client
						</button>
					) : null}
				</div>
				{/* S3: aggregate summary for the current page */}
				{items.length > 0 ? (
					<div className="muted" style={{ marginTop: 8, fontSize: 13 }}>
						{summary.count} shown · {summary.active} active
						{summary.quota > 0
							? ` · total quota ${fmtBytes(summary.quota)}`
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
					<span className="muted">{selected.size} selected</span>
					<button
						type="button"
						className="btn"
						disabled={bulk.isPending}
						onClick={() =>
							bulk.mutate({ action: "enable", ids: [...selected] })
						}
					>
						Enable
					</button>
					<button
						type="button"
						className="btn"
						disabled={bulk.isPending}
						onClick={() =>
							bulk.mutate({ action: "disable", ids: [...selected] })
						}
					>
						Disable
					</button>
					<button
						type="button"
						className="btn"
						disabled={bulk.isPending}
						onClick={() =>
							bulk.mutate({ action: "reset_traffic", ids: [...selected] })
						}
					>
						Reset traffic
					</button>
					{confirmDelete ? (
						<>
							<span className="muted" style={{ fontSize: 13 }}>
								Really delete {selected.size} client
								{selected.size === 1 ? "" : "s"}? This cannot be undone.
							</span>
							<button
								type="button"
								className="btn btn-danger"
								disabled={bulk.isPending}
								onClick={() =>
									bulk.mutate({ action: "delete", ids: [...selected] })
								}
							>
								Confirm delete
							</button>
							<button
								type="button"
								className="btn"
								onClick={() => setConfirmDelete(false)}
							>
								Cancel
							</button>
						</>
					) : (
						<button
							type="button"
							className="btn btn-danger"
							disabled={bulk.isPending}
							onClick={() => setConfirmDelete(true)}
						>
							Delete
						</button>
					)}
					{bulkError ? <span className="form-error">{bulkError}</span> : null}
				</div>
			) : null}

			{/* S3: per-client bulk result */}
			{bulkResults && bulkResults.length > 0 ? (
				<div className="card">
					<h2 style={{ fontSize: 14 }}>Bulk result</h2>
					{bulkResults.map((r) => (
						<div
							key={r.id}
							className="muted"
							style={{ fontSize: 13, display: "flex", gap: 8 }}
						>
							<span
								className={r.ok ? "badge badge-success" : "badge badge-danger"}
							>
								{r.ok ? "ok" : "failed"}
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
					<p className="muted">Loading…</p>
				) : query.isError ? (
					<p className="form-error">
						{query.error instanceof ApiError
							? query.error.message
							: "Failed to load clients"}
					</p>
				) : (
					<div className="table-container">
						<table className="data-table">
							<thead>
								{table.getHeaderGroups().map((hg) => (
									<tr key={hg.id}>
										{hg.headers.map((h) => (
											<th
												key={h.id}
												style={{
													width: h.getSize() !== 150 ? h.getSize() : undefined,
												}}
											>
												{flexRender(h.column.columnDef.header, h.getContext())}
											</th>
										))}
									</tr>
								))}
							</thead>
							<tbody>
								{table.getRowModel().rows.length === 0 ? (
									<tr>
										<td colSpan={columns.length} className="muted">
											No clients found.
										</td>
									</tr>
								) : (
									table.getRowModel().rows.map((row) => (
										<tr
											key={row.id}
											style={{ cursor: "pointer" }}
											onClick={() =>
												void navigate({ to: `/clients/${row.original.id}` })
											}
										>
											{row.getVisibleCells().map((cell) => (
												<td key={cell.id}>
													{flexRender(
														cell.column.columnDef.cell,
														cell.getContext(),
													)}
												</td>
											))}
										</tr>
									))
								)}
							</tbody>
						</table>
					</div>
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
						{total} client{total === 1 ? "" : "s"} · page {page} of {totalPages}
					</span>
					<div style={{ flex: 1 }} />
					<button
						type="button"
						className="btn"
						disabled={page <= 1}
						onClick={() => setParam({ page: String(page - 1) })}
					>
						Previous
					</button>
					<button
						type="button"
						className="btn"
						disabled={page >= totalPages}
						onClick={() => setParam({ page: String(page + 1) })}
					>
						Next
					</button>
				</div>
			</div>
		</>
	);
}
