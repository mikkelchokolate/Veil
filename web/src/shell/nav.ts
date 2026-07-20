// Top-level navigation. Mirrors the legacy panel's section ordering so the
// layout does not change globally. Icons come from lucide-react (tree-shaken,
// no icon-font/CDN — satisfies the strict CSP).
import {
	ArrowLeftRight,
	ChartLine,
	DatabaseBackup,
	Gauge,
	Home,
	ListFilter,
	type LucideIcon,
	Settings,
	Shield,
	SlidersHorizontal,
	Users,
} from "lucide-react";

export interface NavEntry {
	to: string;
	labelKey: string; // i18n key
	icon: LucideIcon;
}

export const NAV_ENTRIES: NavEntry[] = [
	{ to: "/", labelKey: "nav.overview", icon: Home },
	{ to: "/clients", labelKey: "nav.clients", icon: Users },
	{ to: "/inbounds", labelKey: "nav.inbounds", icon: ArrowLeftRight },
	{ to: "/routing", labelKey: "nav.routing", icon: ListFilter },
	{ to: "/traffic", labelKey: "nav.traffic", icon: ChartLine },
	{ to: "/warp", labelKey: "nav.warp", icon: Shield },
	{ to: "/system", labelKey: "nav.system", icon: Gauge },
	{ to: "/backups", labelKey: "nav.backups", icon: DatabaseBackup },
	{ to: "/users", labelKey: "nav.users", icon: Users },
	{ to: "/settings", labelKey: "nav.settings", icon: Settings },
	{ to: "/apply", labelKey: "nav.apply", icon: SlidersHorizontal },
];
