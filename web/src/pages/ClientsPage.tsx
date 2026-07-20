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
} from "@tanstack/react-table";
import { useState } from "react";
import { listClients } from "../api/clients";
import { ApiError } from "../api/fetcher";
import { postApiV1ClientsBulk } from "../api/generated/clients/clients";
import type { ClientView } from "../api/generated/models";
import { useIsAdmin } from "../auth/AuthContext";

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

function fmtBytes(n?: number): string {
	if (n == null) return "—";
	if (n === 0) return "0 B";
	const units = ["B", "KiB", "MiB", "GiB", "TiB"];
	let v = n;
	let i = 0;
	while (v >= 1024 && i < units.length - 1) {
		v /= 1024;
		i++;
	}
	return `${v.toFixed(v >= 10 ? 0 : 1)} ${units[i]}`;
}

function fmtExpiry(ts?: number): string {
	if (!ts) return "—";
	return new Date(ts * 1000).toLocaleDateString();
}

export function ClientsPage() {
	const isAdmin = useIsAdmin();
	const navigate = useNavigate();
	const qc = useQueryClient();
	const search = useSearch({ strict: false }) as Record<
		string,
		string | undefined
	>;

	const page = Number(search.page ?? "1") || 1;
	const pageSize = Number(search.pageSize ?? "25") || 25;
	const searchText = search.search ?? "";
	const status = search.status ?? "";
	const sort = search.sort ?? "created";

	const query = useQuery({
		queryKey: ["clients", "list", { page, pageSize, searchText, status, sort }],
		queryFn: () =>
			listClients({ page, pageSize, search: searchText, status, sort }),
		placeholderData: keepPreviousData,
	});

	const [selected, setSelected] = useState<Set<string>>(new Set());
	const [bulkError, setBulkError] = useState<string | null>(null);
	const [bulkNotice, setBulkNotice] = useState<string | null>(null);

	function setParam(patch: Record<string, string>) {
		void navigate({
			to: "/clients",
			search: (prev: Record<string, string>) => ({ ...prev, ...patch }),
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
			const data = res.data as
				| { succeeded?: number; skipped?: number; failed?: number }
				| undefined;
			if (data) {
				setBulkNotice(
					`succeeded ${data.succeeded ?? 0}, skipped ${data.skipped ?? 0}, failed ${data.failed ?? 0}`,
				);
			}
			void qc.invalidateQueries({ queryKey: ["clients"] });
			void qc.invalidateQueries({ queryKey: ["apply"] });
		},
		onError: (err) => {
			setBulkNotice(null);
			setBulkError(
				err instanceof ApiError ? err.message : "Bulk action failed",
			);
		},
	});

	const items = (query.data?.items ?? []) as ClientView[];
	const total = query.data?.total ?? 0;
	const totalPages = Math.max(1, Math.ceil(total / pageSize));

	// B6: TanStack Table for column definitions + visibility.
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
						defaultValue={searchText}
						onKeyDown={(e) => {
							if (e.key === "Enter") {
								setParam({
									search: (e.target as HTMLInputElement).value,
									page: "1",
								});
							}
						}}
					/>
					<select
						className="input"
						style={{ maxWidth: 160 }}
						value={status}
						onChange={(e) => setParam({ status: e.target.value, page: "1" })}
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
					>
						<option value="created">Newest</option>
						<option value="name">Name</option>
						<option value="expires">Expiry</option>
					</select>
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
			</div>

			{isAdmin && selected.size > 0 ? (
				<div
					className="card"
					style={{ display: "flex", gap: 8, alignItems: "center" }}
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
					<button
						type="button"
						className="btn btn-danger"
						disabled={bulk.isPending}
						onClick={() =>
							bulk.mutate({ action: "delete", ids: [...selected] })
						}
					>
						Delete
					</button>
					{bulkNotice ? <span className="muted">{bulkNotice}</span> : null}
					{bulkError ? <span className="form-error">{bulkError}</span> : null}
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
