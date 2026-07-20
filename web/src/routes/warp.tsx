import { createFileRoute } from "@tanstack/react-router";
import { WarpPage } from "../pages/WarpPage";

export const Route = createFileRoute("/warp")({
	component: WarpPage,
});
