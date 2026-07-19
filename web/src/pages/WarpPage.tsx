import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch, ApiError } from "../api/fetcher";
import { useIsAdmin } from "../auth/AuthContext";
import type { WarpConfig } from "../api/generated/models";

/** WARP outbound: enable/disable toggle + read-only status. Provisioning of a
 * free Cloudflare account is server-side on enable. */
export function WarpPage() {
	const isAdmin = useIsAdmin();
	const qc = useQueryClient();

	const warp = useQuery<WarpConfig>({
		queryKey: ["warp"],
		queryFn: () => apiFetch("/api/warp"),
	});

	const toggle = useMutation({
		mutationFn: (enabled: boolean) =>
			apiFetch("/api/warp", {
				method: "PUT",
				body: JSON.stringify({ enabled }),
			}),
		onSuccess: () => void qc.invalidateQueries({ queryKey: ["warp"] }),
	});

	if (warp.isLoading) {
		return <div className="card"><p className="muted">Loading…</p></div>;
	}
	if (warp.isError || !warp.data) {
		return (
			<div className="card">
				<p className="form-error">
					{warp.error instanceof ApiError ? warp.error.message : "WARP config unavailable"}
				</p>
			</div>
		);
	}

	const w = warp.data;
	return (
		<div className="card">
			<h2>WARP outbound</h2>
			<p>
				<strong>Status:</strong>{" "}
				<span className={`badge${w.enabled ? " badge-success" : ""}`}>
					{w.enabled ? "enabled" : "disabled"}
				</span>
			</p>
			{w.endpoint ? <p><strong>Endpoint:</strong> <span className="mono muted">{w.endpoint}</span></p> : null}
			{w.localAddress ? <p><strong>Local address:</strong> <span className="mono muted">{w.localAddress}</span></p> : null}
			{w.socksListen ? (
				<p><strong>SOCKS:</strong> <span className="mono muted">{w.socksListen}{w.socksPort ? `:${w.socksPort}` : ""}</span></p>
			) : null}
			{w.mtu ? <p><strong>MTU:</strong> <span className="muted">{w.mtu}</span></p> : null}
			{isAdmin ? (
				<button
					type="button"
					className={`btn ${w.enabled ? "" : "btn-primary"}`}
					disabled={toggle.isPending}
					onClick={() => toggle.mutate(!w.enabled)}
				>
					{toggle.isPending ? "Applying…" : w.enabled ? "Disable WARP" : "Enable WARP"}
				</button>
			) : null}
			{toggle.isError ? (
				<p className="form-error">
					{toggle.error instanceof ApiError ? toggle.error.message : "Toggle failed"}
				</p>
			) : null}
			<p className="muted" style={{ fontSize: 12, marginTop: 8 }}>
				Enabling provisions a free Cloudflare WARP account server-side; changes apply on the next
				config apply.
			</p>
		</div>
	);
}
