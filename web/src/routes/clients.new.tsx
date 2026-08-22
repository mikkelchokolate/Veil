import { createFileRoute } from "@tanstack/react-router";
import { ClientNewPage } from "../pages/ClientNewPage";

export const Route = createFileRoute("/clients/new")({
	component: ClientNewPage,
});
