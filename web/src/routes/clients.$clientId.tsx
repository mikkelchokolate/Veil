import { createFileRoute } from "@tanstack/react-router";
import { ClientDetailPage } from "../pages/ClientDetailPage";

export const Route = createFileRoute("/clients/$clientId")({
	component: ClientDetailPage,
});
