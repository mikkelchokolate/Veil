import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { ClientsPage } from "../pages/ClientsPage";

// Search validation lives in the file route itself (blocker W6): the URL is
// user-editable input, so every param is parsed/coerced/defaulted here. Each
// field is optional-with-catch: an absent param stays undefined (the page
// applies defaults), while a PRESENT but invalid value fails parse and is
// caught to the default — a hand-edited URL can never put the page in an
// invalid state.
export const clientsSearchSchema = z.object({
	page: z.coerce.number().int().positive().optional().catch(1),
	pageSize: z.coerce.number().int().positive().max(200).optional().catch(25),
	search: z.string().optional().catch(""),
	status: z.string().optional().catch(""),
	inboundId: z.string().optional().catch(""),
	sort: z.string().optional().catch("created"),
});

export const Route = createFileRoute("/clients/")({
	validateSearch: (search: Record<string, unknown>) =>
		clientsSearchSchema.parse(search),
	component: ClientsPage,
});
