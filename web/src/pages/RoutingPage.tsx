import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, apiFetch } from "../api/fetcher";
import type { RoutingRule } from "../api/generated/models";
import { useIsAdmin } from "../auth/AuthContext";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { FormItem, FormMessage } from "../components/ui/form";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "../components/ui/table";

/** Routing rules: list + create/edit/delete mutations. */
export function RoutingPage() {
	const isAdmin = useIsAdmin();
	const qc = useQueryClient();
	const [editing, setEditing] = useState<RoutingRule | null>(null);
	const [creating, setCreating] = useState(false);
	const [form, setForm] = useState({
		name: "",
		match: "",
		outbound: "",
		enabled: true,
	});

	const rules = useQuery<RoutingRule[]>({
		queryKey: ["routing", "rules"],
		queryFn: () => apiFetch("/api/routing/rules"),
	});

	const save = useMutation({
		mutationFn: (rule: RoutingRule) => {
			if (creating) {
				return apiFetch("/api/routing/rules", {
					method: "POST",
					body: JSON.stringify(rule),
				});
			}
			return apiFetch(`/api/routing/rules/${encodeURIComponent(rule.name)}`, {
				method: "PUT",
				body: JSON.stringify(rule),
			});
		},
		onSuccess: () => {
			setEditing(null);
			setCreating(false);
			setForm({ name: "", match: "", outbound: "", enabled: true });
			void qc.invalidateQueries({ queryKey: ["routing"] });
		},
	});

	const del = useMutation({
		mutationFn: (name: string) =>
			apiFetch(`/api/routing/rules/${encodeURIComponent(name)}`, {
				method: "DELETE",
			}),
		onSuccess: () => void qc.invalidateQueries({ queryKey: ["routing"] }),
	});

	function startEdit(r: RoutingRule) {
		setEditing(r);
		setCreating(false);
		setForm({
			name: r.name,
			match: r.match,
			outbound: r.outbound,
			enabled: r.enabled,
		});
	}

	function startCreate() {
		setEditing(null);
		setCreating(true);
		setForm({ name: "", match: "", outbound: "", enabled: true });
	}

	function cancel() {
		setEditing(null);
		setCreating(false);
		setForm({ name: "", match: "", outbound: "", enabled: true });
	}

	return (
		<>
			<div className="card">
				<div style={{ display: "flex", gap: 12, alignItems: "center" }}>
					<h2 style={{ margin: 0, flex: 1 }}>Routing rules</h2>
					{isAdmin ? (
						<Button variant="primary" onClick={startCreate}>
							New rule
						</Button>
					) : null}
				</div>
			</div>

			{creating || editing ? (
				<div className="card">
					<h3>{creating ? "Create rule" : `Edit ${editing?.name}`}</h3>
					<div style={{ display: "grid", gap: 12, maxWidth: 480 }}>
						<FormItem>
							<Label htmlFor="rule-name">Name</Label>
							<Input
								id="rule-name"
								value={form.name}
								onChange={(e) => setForm({ ...form, name: e.target.value })}
								disabled={!creating}
							/>
						</FormItem>
						<FormItem>
							<Label htmlFor="rule-match">Match (CIDR, domain, or geoip)</Label>
							<Input
								id="rule-match"
								value={form.match}
								onChange={(e) => setForm({ ...form, match: e.target.value })}
							/>
						</FormItem>
						<FormItem>
							<Label htmlFor="rule-outbound">Outbound</Label>
							<Input
								id="rule-outbound"
								value={form.outbound}
								onChange={(e) => setForm({ ...form, outbound: e.target.value })}
							/>
						</FormItem>
						<FormItem>
							<Label
								htmlFor="rule-enabled"
								style={{ display: "flex", gap: 8, alignItems: "center" }}
							>
								<input
									id="rule-enabled"
									type="checkbox"
									checked={form.enabled}
									onChange={(e) =>
										setForm({ ...form, enabled: e.target.checked })
									}
								/>
								Enabled
							</Label>
						</FormItem>
						<div style={{ display: "flex", gap: 8 }}>
							<Button
								variant="primary"
								disabled={
									save.isPending || !form.name || !form.match || !form.outbound
								}
								onClick={() =>
									save.mutate({
										name: form.name,
										match: form.match,
										outbound: form.outbound,
										enabled: form.enabled,
									})
								}
							>
								{save.isPending ? "Saving…" : "Save"}
							</Button>
							<Button onClick={cancel}>Cancel</Button>
						</div>
						{save.isError ? (
							<FormMessage>
								{save.error instanceof ApiError
									? save.error.message
									: "Save failed"}
							</FormMessage>
						) : null}
					</div>
				</div>
			) : null}

			<div className="card">
				{rules.isLoading ? (
					<p className="muted">Loading…</p>
				) : rules.isError ? (
					<FormMessage>
						{rules.error instanceof ApiError
							? rules.error.message
							: "Failed to load routing rules"}
					</FormMessage>
				) : (
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>Name</TableHead>
								<TableHead>Match</TableHead>
								<TableHead>Outbound</TableHead>
								<TableHead>Status</TableHead>
								{isAdmin ? <TableHead /> : null}
							</TableRow>
						</TableHeader>
						<TableBody>
							{(rules.data ?? []).length === 0 ? (
								<TableRow>
									<TableCell colSpan={isAdmin ? 5 : 4} className="muted">
										No routing rules configured.
									</TableCell>
								</TableRow>
							) : (
								(rules.data ?? []).map((r) => (
									<TableRow key={r.name}>
										<TableCell>{r.name}</TableCell>
										<TableCell className="muted">{r.match}</TableCell>
										<TableCell className="muted">{r.outbound}</TableCell>
										<TableCell>
											<Badge variant={r.enabled ? "success" : "default"}>
												{r.enabled ? "enabled" : "disabled"}
											</Badge>
										</TableCell>
										{isAdmin ? (
											<TableCell style={{ display: "flex", gap: 4 }}>
												<Button size="sm" onClick={() => startEdit(r)}>
													Edit
												</Button>
												<Button
													size="sm"
													variant="danger"
													disabled={del.isPending}
													onClick={() => del.mutate(r.name)}
												>
													Delete
												</Button>
											</TableCell>
										) : null}
									</TableRow>
								))
							)}
						</TableBody>
					</Table>
				)}
			</div>
		</>
	);
}
