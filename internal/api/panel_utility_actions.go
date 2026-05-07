package api

const panelUtilityActionsPlaceholder = "__VEIL_PANEL_UTILITY_ACTIONS__"

func panelUtilityActionsJS() string {
	return `    function parseReserved(value) {
      if (!value.trim()) {
        return [];
      }
      return value.split(',').map((part) => Number(part.trim())).filter((value) => Number.isInteger(value));
    }

    function numberOrZero(id) {
      const value = document.getElementById(id).value;
      return value === '' ? 0 : Number(value);
    }`
}
