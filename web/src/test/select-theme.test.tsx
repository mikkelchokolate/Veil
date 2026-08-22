import { render } from "@testing-library/react";
import { Select } from "../components/ui/select";

test("select width constraints sit on the wrapper so the chevron stays on the control", () => {
	const { container } = render(
		<Select aria-label="status" style={{ maxWidth: 160 }}>
			<option value="all">all</option>
		</Select>,
	);
	const wrapper = container.firstElementChild as HTMLElement;
	const select = container.querySelector("select");
	if (!wrapper || !select) {
		throw new Error("missing select markup");
	}
	expect(wrapper).toHaveStyle({ maxWidth: "160px" });
	expect(select).not.toHaveStyle({ maxWidth: "160px" });
	expect(select.className).toMatch(/appearance-none/);
	expect(select.className).toMatch(/rounded-none/);
});
