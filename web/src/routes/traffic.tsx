import { createFileRoute } from "@tanstack/react-router";
import { TrafficPage } from "../pages/TrafficPage";

export const Route = createFileRoute("/traffic")({
	component: TrafficPage,
});
