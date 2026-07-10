package panel

import "strings"

func panelWarpHardenedCardHTML() string {
	return strings.Replace(
		panelWarpCardHTML(),
		`<button class="btn-console-clear" type="button" onclick="document.getElementById('warp-output').textContent = 'Console cleared.'">`,
		`<button id="clear-warp-output" class="btn-console-clear" type="button">`,
		1,
	)
}

func panelWarpControlsJS() string {
	return `
    document.getElementById('clear-warp-output').addEventListener('click', () => {
      const output = document.getElementById('warp-output');
      if (output) output.textContent = 'Console cleared.';
    });
`
}
