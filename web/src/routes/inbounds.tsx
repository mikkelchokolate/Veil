import { createFileRoute } from "@tanstack/react-router";
import { InboundsPage } from "../pages/InboundsPage";

export const Route = createFileRoute("/inbounds")({
	component: InboundsPage,
});
