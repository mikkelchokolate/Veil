import { createFileRoute } from "@tanstack/react-router";
import { BackupsPage } from "../pages/BackupsPage";

export const Route = createFileRoute("/backups")({
	component: BackupsPage,
});
