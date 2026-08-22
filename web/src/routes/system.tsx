import { createFileRoute } from "@tanstack/react-router";
import { SystemPage } from "../pages/SystemPage";

export const Route = createFileRoute("/system")({
	component: SystemPage,
});
