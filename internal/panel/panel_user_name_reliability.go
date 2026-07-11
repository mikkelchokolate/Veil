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

    function validPanelUserPassword(password) {
      return new TextEncoder().encode(String(password || '')).length >= 12;
    }

    const userNameInput = document.getElementById('user-username');
    if (userNameInput) {
      userNameInput.minLength = 3;
      userNameInput.maxLength = 64;
      userNameInput.addEventListener('input', () => userNameInput.setCustomValidity(''));
    }
    const userPasswordInput = document.getElementById('user-password');
    if (userPasswordInput) {
      userPasswordInput.minLength = 12;
      userPasswordInput.addEventListener('input', () => userPasswordInput.setCustomValidity(''));
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

      const passwordInput = document.getElementById('user-password');
      const password = passwordInput ? passwordInput.value : '';
      const isEdit = document.getElementById('user-is-edit').value === 'true';
      if ((!isEdit || password) && !validPanelUserPassword(password)) {
        const message = 'Password must be at least 12 characters.';
        if (passwordInput) {
          passwordInput.setCustomValidity(message);
          passwordInput.reportValidity();
        }
        const output = document.getElementById('user-output');
        if (output) output.textContent = message;
        return null;
      }
      if (passwordInput) passwordInput.setCustomValidity('');
      return baseSaveUserWithNameValidation(event);
    };
`
}
