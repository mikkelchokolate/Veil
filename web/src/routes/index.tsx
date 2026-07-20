import { createFileRoute } from "@tanstack/react-router";
import { OverviewPage } from "../pages/OverviewPage";

export const Route = createFileRoute("/")({
	component: OverviewPage,
});
