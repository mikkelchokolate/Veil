package panel

func panelUserNameReliabilityJS() string {
	return `
    function validPanelUsername(username) {
      const value = String(username || '');
      const byteLength = new TextEncoder().encode(value).length;
      return byteLength >= 3
        && byteLength <= 64
        && /^[\p{L}\p{N}._-]+$/u.test(value);
    }

    const userNameInput = document.getElementById('user-username');
    if (userNameInput) {
      userNameInput.minLength = 3;
      userNameInput.maxLength = 64;
      userNameInput.addEventListener('input', () => userNameInput.setCustomValidity(''));
    }

    const baseSaveUserWithNameValidation = saveUser;
    saveUser = async function(event) {
      if (event) event.preventDefault();
      const input = document.getElementById('user-username');
      const username = input ? input.value.trim() : '';
      if (!validPanelUsername(username)) {
        const message = 'Username must be 3-64 characters using letters, digits, dot, underscore, or hyphen.';
        if (input) {
          input.setCustomValidity(message);
          input.reportValidity();
        }
        const output = document.getElementById('user-output');
        if (output) output.textContent = message;
        return null;
      }
      if (input) input.setCustomValidity('');
      return baseSaveUserWithNameValidation(event);
    };
`
}
