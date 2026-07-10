package panel

const panelUtilityActionsPlaceholder = "__VEIL_PANEL_UTILITY_ACTIONS__"

func panelUtilityActionsJS() string {
	return `    let veilActiveDialog = null;
    let veilPreviouslyFocused = null;

    function veilDialogFocusableElements(dialog) {
      return Array.from(dialog.querySelectorAll(
        'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
      )).filter((element) => !element.hidden && element.getAttribute('aria-hidden') !== 'true');
    }

    function openVeilDialog(dialog) {
      if (!dialog) return;
      veilPreviouslyFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      veilActiveDialog = dialog;
      dialog.classList.add('active');
      dialog.setAttribute('aria-hidden', 'false');
      const content = dialog.querySelector('[role="dialog"]') || dialog;
      const focusableElements = veilDialogFocusableElements(content);
      window.requestAnimationFrame(() => {
        (focusableElements[0] || content).focus();
      });
    }

    function closeVeilDialog(dialog) {
      if (!dialog) return;
      dialog.classList.remove('active');
      dialog.setAttribute('aria-hidden', 'true');
      if (veilActiveDialog === dialog) {
        veilActiveDialog = null;
      }
      if (veilPreviouslyFocused && document.contains(veilPreviouslyFocused)) {
        veilPreviouslyFocused.focus();
      }
      veilPreviouslyFocused = null;
    }

    document.addEventListener('keydown', (event) => {
      if (!veilActiveDialog) return;
      if (event.key === 'Escape') {
        event.preventDefault();
        const closeButton = veilActiveDialog.querySelector('.modal-close');
        if (closeButton) {
          closeButton.click();
        } else {
          closeVeilDialog(veilActiveDialog);
        }
        return;
      }
      if (event.key !== 'Tab') return;
      const focusableElements = veilDialogFocusableElements(veilActiveDialog);
      if (focusableElements.length === 0) {
        event.preventDefault();
        const content = veilActiveDialog.querySelector('[role="dialog"]') || veilActiveDialog;
        content.focus();
        return;
      }
      const first = focusableElements[0];
      const last = focusableElements[focusableElements.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    });

    function parseReserved(value) {
      const raw = String(value || '').trim();
      if (!raw) return [];
      const parts = raw.split(',').map((part) => part.trim());
      if (parts.length !== 3 || parts.some((part) => part === '')) {
        throw new Error('Reserved bytes must contain exactly three comma-separated integers.');
      }
      const reserved = parts.map((part) => Number(part));
      if (reserved.some((byte) => !Number.isInteger(byte) || byte < 0 || byte > 255)) {
        throw new Error('Reserved bytes must be integers between 0 and 255.');
      }
      return reserved;
    }

    function numberOrZero(id) {
      const input = document.getElementById(id);
      const raw = input ? input.value.trim() : '';
      if (raw === '') return 0;
      if (!input.checkValidity()) {
        input.reportValidity();
        throw new Error('Invalid numeric value for ' + id + '.');
      }
      const value = Number(raw);
      if (!Number.isInteger(value)) {
        throw new Error('Numeric value for ' + id + ' must be a whole number.');
      }
      return value;
    }`
}
