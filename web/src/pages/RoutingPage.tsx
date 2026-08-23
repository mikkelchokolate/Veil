import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { apiFetch, mutationErrorMessage } from "../api/fetcher";
import type { RoutingRule } from "../api/generated/models";
import { useIsAdmin } from "../auth/AuthContext";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "../components/ui/dialog";
import { FormDescription, FormItem, FormMessage } from "../components/ui/form";
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
import { useI18n } from "../i18n/I18nContext";

interface WarpStatus {
	enabled?: boolean;
}

/** Routing rules: list + create/edit/delete mutations. */
export function RoutingPage() {
	const { t } = useI18n();
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
	const warp = useQuery<WarpStatus>({
		queryKey: ["warp"],
		queryFn: () => apiFetch("/api/warp"),
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
					<h2 style={{ margin: 0, flex: 1 }}>{t("routing.title")}</h2>
					{isAdmin ? (
						<Button variant="primary" onClick={startCreate}>
							{t("routing.newRule")}
						</Button>
					) : null}
				</div>
				{warp.data && !warp.data.enabled ? (
					<p className="muted" style={{ fontSize: 13, marginTop: 12 }}>
						{t("routing.warpRequired")}
					</p>
				) : null}
				{del.isError ? (
					<FormMessage>
						{mutationErrorMessage(del.error, t("routing.deleteFailed"))}
					</FormMessage>
				) : null}
			</div>

			<Dialog
				open={creating || editing !== null}
				onOpenChange={(open) => {
					if (!open) cancel();
				}}
			>
				<DialogContent className="creation-dialog">
					<div className="card">
						<DialogTitle>
							{creating
								? t("routing.createRule")
								: t("routing.editRule", { name: editing?.name ?? "" })}
						</DialogTitle>
						<div style={{ display: "grid", gap: 12, maxWidth: 480 }}>
							<FormItem>
								<Label htmlFor="rule-name">{t("routing.name")}</Label>
								<Input
									id="rule-name"
									value={form.name}
									onChange={(e) => setForm({ ...form, name: e.target.value })}
									disabled={!creating}
								/>
							</FormItem>
							<FormItem>
								<Label htmlFor="rule-match">{t("routing.match")}</Label>
								<Input
									id="rule-match"
									value={form.match}
									onChange={(e) => setForm({ ...form, match: e.target.value })}
								/>
								<FormDescription>{t("routing.matchHint")}</FormDescription>
							</FormItem>
							<FormItem>
								<Label htmlFor="rule-outbound">{t("routing.outbound")}</Label>
								<Input
									id="rule-outbound"
									value={form.outbound}
									onChange={(e) =>
										setForm({ ...form, outbound: e.target.value })
									}
								/>
								<FormDescription>{t("routing.outboundHint")}</FormDescription>
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
									{t("routing.enabled")}
								</Label>
							</FormItem>
							<div style={{ display: "flex", gap: 8 }}>
								<Button
									variant="primary"
									disabled={
										save.isPending ||
										!form.name ||
										!form.match ||
										!form.outbound
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
									{save.isPending ? t("routing.saving") : t("common.save")}
								</Button>
								<Button onClick={cancel}>{t("common.cancel")}</Button>
							</div>
							{save.isError ? (
								<FormMessage>
									{mutationErrorMessage(save.error, t("routing.saveFailed"))}
								</FormMessage>
							) : null}
						</div>
					</div>
				</DialogContent>
			</Dialog>

			<div className="card">
				{rules.isLoading ? (
					<p className="muted">{t("common.loading")}</p>
				) : rules.isError ? (
					<FormMessage>
						{mutationErrorMessage(rules.error, t("routing.loadFailed"))}
					</FormMessage>
				) : (
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>{t("common.name")}</TableHead>
								<TableHead>{t("routing.match")}</TableHead>
								<TableHead>{t("routing.outbound")}</TableHead>
								<TableHead>{t("common.status")}</TableHead>
								{isAdmin ? <TableHead /> : null}
							</TableRow>
						</TableHeader>
						<TableBody>
							{(rules.data ?? []).length === 0 ? (
								<TableRow>
									<TableCell colSpan={isAdmin ? 5 : 4} className="muted">
										{t("routing.empty")}
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
												{r.enabled ? t("common.enabled") : t("common.disabled")}
											</Badge>
										</TableCell>
										{isAdmin ? (
											<TableCell style={{ display: "flex", gap: 4 }}>
												<Button size="sm" onClick={() => startEdit(r)}>
													{t("common.edit")}
												</Button>
												<Button
													size="sm"
													variant="danger"
													disabled={del.isPending}
													onClick={() => del.mutate(r.name)}
												>
													{t("common.delete")}
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
