import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { ApiError, apiFetch } from "../api/fetcher";

interface InboundOption {
	name: string;
	protocol: string;
	enabled?: boolean;
}

interface BindingDraft {
	inboundId: string;
	credential: string;
}

interface CreatedCredential {
	bindingId: string;
	inboundId: string;
	plaintext?: string | undefined;
}

/** B6/B7: create form with General / Limits / Access (bindings+credentials) /
 * Review steps. Server-generated credentials are shown once in the review. */
export function ClientNewPage() {
	const navigate = useNavigate();
	const qc = useQueryClient();

	const inbounds = useQuery<{ items?: InboundOption[] } | InboundOption[]>({
		queryKey: ["inbounds", "all"],
		queryFn: () => apiFetch("/api/inbounds"),
	});

	const [step, setStep] = useState(0);
	const [name, setName] = useState("");
	const [email, setEmail] = useState("");
	const [notes, setNotes] = useState("");
	const [quotaBytes, setQuotaBytes] = useState("");
	const [expiresAt, setExpiresAt] = useState("");
	const [bindings, setBindings] = useState<BindingDraft[]>([]);
	const [createdCreds, setCreatedCreds] = useState<CreatedCredential[]>([]);
	const [error, setError] = useState<string | null>(null);

	const inboundList: InboundOption[] = Array.isArray(inbounds.data)
		? inbounds.data
		: (inbounds.data?.items ?? []);

	const create = useMutation({
		mutationFn: async () => {
			const body: Record<string, unknown> = {
				name,
				enabled: true,
			};
			if (email) body.email = email;
			if (notes) body.notes = notes;
			if (quotaBytes) body.quotaBytes = Number(quotaBytes);
			if (expiresAt)
				body.expiresAt = Math.floor(new Date(expiresAt).getTime() / 1000);
			if (bindings.length > 0) {
				body.bindings = bindings.map((b) => ({
					inboundId: b.inboundId,
					...(b.credential ? { credential: b.credential } : {}),
				}));
			}
			return apiFetch<{ id: string }>("/api/v1/clients", {
				method: "POST",
				body: JSON.stringify(body),
			});
		},
		onSuccess: async (client) => {
			// Rotate server-side to obtain one-time plaintext for each credential
			// we did not explicitly set, so the operator can hand it to the user.
			const revealed: CreatedCredential[] = [];
			for (const b of bindings) {
				if (b.credential) continue; // operator-provided; already known to them
				try {
					const res = await apiFetch<
						CreatedCredential & { plaintext?: string }
					>(
						`/api/v1/clients/${client.id}/credentials/${encodeURIComponent(b.inboundId)}/rotate`,
						{ method: "POST", body: JSON.stringify({}) },
					);
					revealed.push({
						bindingId: b.inboundId,
						inboundId: b.inboundId,
						plaintext: res.plaintext,
					});
				} catch {
					// non-fatal: credential exists, just not revealed here
				}
			}
			setCreatedCreds(revealed);
			void qc.invalidateQueries({ queryKey: ["clients"] });
			void qc.invalidateQueries({ queryKey: ["apply"] });
			setStep(3);
		},
		onError: (err) => {
			setError(
				err instanceof ApiError ? err.message : "Failed to create client",
			);
		},
	});

	function toggleBinding(inboundId: string) {
		setBindings((prev) =>
			prev.some((b) => b.inboundId === inboundId)
				? prev.filter((b) => b.inboundId !== inboundId)
				: [...prev, { inboundId, credential: "" }],
		);
	}

	function setCred(inboundId: string, credential: string) {
		setBindings((prev) =>
			prev.map((b) => (b.inboundId === inboundId ? { ...b, credential } : b)),
		);
	}

	function onSubmit(e: FormEvent) {
		e.preventDefault();
		setError(null);
		create.mutate();
	}

	const steps = ["General", "Limits", "Access", "Review"];

	return (
		<div className="card" style={{ maxWidth: 640 }}>
			<h2>New client</h2>
			<div className="muted" style={{ marginBottom: 20, fontSize: 13 }}>
				{steps.map((s, i) => (
					<span
						key={s}
						style={{
							fontWeight: i === step ? 700 : 400,
							color: i === step ? "var(--text-main)" : undefined,
						}}
					>
						{i > 0 ? " · " : ""}
						{s}
					</span>
				))}
			</div>

			{step === 0 ? (
				<>
					<div className="form-field">
						<label htmlFor="nc-name">Name</label>
						<input
							id="nc-name"
							className="input"
							value={name}
							onChange={(e) => setName(e.target.value)}
							required
						/>
					</div>
					<div className="form-field">
						<label htmlFor="nc-email">Email (optional)</label>
						<input
							id="nc-email"
							className="input"
							type="email"
							value={email}
							onChange={(e) => setEmail(e.target.value)}
						/>
					</div>
					<div className="form-field">
						<label htmlFor="nc-notes">Notes (optional)</label>
						<textarea
							id="nc-notes"
							className="input"
							value={notes}
							onChange={(e) => setNotes(e.target.value)}
						/>
					</div>
				</>
			) : null}

			{step === 1 ? (
				<>
					<div className="form-field">
						<label htmlFor="nc-quota">Quota (bytes, optional)</label>
						<input
							id="nc-quota"
							className="input"
							type="number"
							min="0"
							value={quotaBytes}
							onChange={(e) => setQuotaBytes(e.target.value)}
						/>
					</div>
					<div className="form-field">
						<label htmlFor="nc-exp">Expiry date (optional)</label>
						<input
							id="nc-exp"
							className="input"
							type="date"
							value={expiresAt}
							onChange={(e) => setExpiresAt(e.target.value)}
						/>
					</div>
				</>
			) : null}

			{step === 2 ? (
				<div className="form-field">
					<label>Bind to inbounds</label>
					{inboundList.length === 0 ? (
						<p className="muted">No inbounds available.</p>
					) : (
						inboundList.map((ib) => {
							const bound = bindings.find((b) => b.inboundId === ib.name);
							return (
								<div
									key={ib.name}
									style={{
										border: "1px solid var(--border)",
										borderRadius: 6,
										padding: 12,
										marginBottom: 8,
									}}
								>
									<label
										style={{
											display: "flex",
											alignItems: "center",
											gap: 8,
											cursor: "pointer",
										}}
									>
										<input
											type="checkbox"
											checked={!!bound}
											onChange={() => toggleBinding(ib.name)}
										/>
										<span>{ib.name}</span>
										<span className="muted" style={{ fontSize: 12 }}>
											{ib.protocol}
										</span>
									</label>
									{bound ? (
										<input
											className="input mono"
											style={{ marginTop: 8 }}
											placeholder="Credential (blank = server generates)"
											value={bound.credential}
											onChange={(e) => setCred(ib.name, e.target.value)}
										/>
									) : null}
								</div>
							);
						})
					)}
				</div>
			) : null}

			{step === 3 ? (
				<div>
					{create.isSuccess ? (
						<>
							<p className="badge badge-success">Client created</p>
							{createdCreds.length > 0 ? (
								<div className="card" style={{ marginTop: 12 }}>
									<h2 style={{ fontSize: 14 }}>One-time credentials</h2>
									<p className="muted" style={{ fontSize: 13 }}>
										Copy these now — they are shown only once.
									</p>
									{createdCreds.map((c) => (
										<div key={c.inboundId} style={{ marginBottom: 8 }}>
											<div className="muted" style={{ fontSize: 12 }}>
												{c.inboundId}
											</div>
											<code className="mono">
												{c.plaintext ?? "(unavailable)"}
											</code>
										</div>
									))}
								</div>
							) : null}
							<div style={{ display: "flex", gap: 8, marginTop: 16 }}>
								<button
									type="button"
									className="btn btn-primary"
									onClick={() => void navigate({ to: "/clients" })}
								>
									Done
								</button>
							</div>
						</>
					) : (
						<>
							<h2 style={{ fontSize: 14 }}>Review</h2>
							<p>
								<strong>Name:</strong> {name}
							</p>
							{email ? (
								<p>
									<strong>Email:</strong> {email}
								</p>
							) : null}
							{quotaBytes ? (
								<p>
									<strong>Quota:</strong> {quotaBytes} bytes
								</p>
							) : null}
							{expiresAt ? (
								<p>
									<strong>Expires:</strong> {expiresAt}
								</p>
							) : null}
							<p>
								<strong>Bindings:</strong> {bindings.length}
							</p>
						</>
					)}
				</div>
			) : null}

			{error ? (
				<div className="form-error" role="alert" style={{ marginTop: 8 }}>
					{error}
				</div>
			) : null}

			{step < 3 || !create.isSuccess ? (
				<form
					onSubmit={onSubmit}
					style={{ display: "flex", gap: 8, marginTop: 20 }}
				>
					<button
						type="button"
						className="btn"
						disabled={step === 0 || create.isPending}
						onClick={() => setStep((s) => Math.max(0, s - 1))}
					>
						Back
					</button>
					{step < 2 ? (
						<button
							type="button"
							className="btn btn-primary"
							disabled={step === 0 && !name}
							onClick={() => setStep((s) => s + 1)}
						>
							Next
						</button>
					) : step === 2 ? (
						<button
							type="button"
							className="btn btn-primary"
							onClick={() => setStep(3)}
						>
							Review
						</button>
					) : (
						<button
							type="submit"
							className="btn btn-primary"
							disabled={create.isPending}
						>
							{create.isPending ? "Creating…" : "Create client"}
						</button>
					)}
				</form>
			) : null}
		</div>
	);
}
