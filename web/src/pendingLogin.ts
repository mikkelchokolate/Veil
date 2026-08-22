export type PendingLogin = {
	username: string;
	password: string;
	submit: boolean;
};

declare global {
	interface Window {
		__VEIL_PENDING_LOGIN?: PendingLogin;
	}
}

export function captureLoginFields(submit: boolean): void {
	const user = document.getElementById("login-username");
	const pass = document.getElementById("login-password");
	const username = user instanceof HTMLInputElement ? user.value : "";
	const password = pass instanceof HTMLInputElement ? pass.value : "";
	if (!username && !password && !submit) return;
	const prev = window.__VEIL_PENDING_LOGIN;
	window.__VEIL_PENDING_LOGIN = {
		username,
		password,
		submit: submit || Boolean(prev?.submit),
	};
}

export function takePendingLogin(): PendingLogin {
	const pending = window.__VEIL_PENDING_LOGIN;
	delete window.__VEIL_PENDING_LOGIN;
	return pending ?? { username: "", password: "", submit: false };
}
