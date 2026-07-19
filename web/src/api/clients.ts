import { apiFetch } from "./fetcher";

// Hand-typed wrappers for endpoints whose OpenAPI surface under-declares the
// real query params / response shape (recorded as spec gaps to reconcile).
// These still flow through the same central fetcher (CSRF, base path, errors).

export interface ClientListParams {
	page?: number;
	pageSize?: number;
	search?: string;
	status?: string;
	quotaState?: string;
	sort?: string;
	inboundId?: string;
}

export interface ClientListResult<T = unknown> {
	items: T[];
	total: number;
	page: number;
	pageSize: number;
}

export function listClients(params: ClientListParams): Promise<ClientListResult> {
	const qs = new URLSearchParams();
	if (params.page) qs.set("page", String(params.page));
	if (params.pageSize) qs.set("pageSize", String(params.pageSize));
	if (params.search) qs.set("search", params.search);
	if (params.status) qs.set("status", params.status);
	if (params.quotaState) qs.set("quotaState", params.quotaState);
	if (params.sort) qs.set("sort", params.sort);
	if (params.inboundId) qs.set("inboundId", params.inboundId);
	const suffix = qs.size ? `?${qs.toString()}` : "";
	return apiFetch<ClientListResult>(`/api/v1/clients${suffix}`);
}
