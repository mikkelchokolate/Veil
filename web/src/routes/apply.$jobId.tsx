import { createFileRoute } from "@tanstack/react-router";
import { ApplyJobDetailPage } from "../pages/ApplyJobDetailPage";

export const Route = createFileRoute("/apply/$jobId")({
	component: ApplyJobDetailPage,
});
