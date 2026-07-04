package olcrtc

import (
	"errors"
	"io"
	"math/big"
	"testing"
)

func setRandInt(t *testing.T, f func(io.Reader, *big.Int) (*big.Int, error)) {
	orig := randInt
	randInt = f
	t.Cleanup(func() { randInt = orig })
}

func setCryptoIntn(t *testing.T, f func(int) (int, error)) {
	orig := cryptoIntn
	cryptoIntn = f
	t.Cleanup(func() { cryptoIntn = orig })
}

func setRandRead(t *testing.T, f func([]byte) (int, error)) {
	orig := randRead
	randRead = f
	t.Cleanup(func() { randRead = orig })
}

func TestCryptoIntnError(t *testing.T) {
	setRandInt(t, func(io.Reader, *big.Int) (*big.Int, error) {
		return nil, errors.New("injected rand.Int error")
	})
	if _, err := cryptoIntn(10); err == nil {
		t.Fatal("expected error from cryptoIntn")
	}
}

func TestRandomRoomNameCryptoIntnError(t *testing.T) {
	setCryptoIntn(t, func(int) (int, error) {
		return 0, errors.New("injected cryptoIntn error")
	})
	if _, err := randomRoomName(); err == nil {
		t.Fatal("expected error from randomRoomName")
	}
}

func TestRandomRoomNameSchemeOneError(t *testing.T) {
	calls := 0
	setCryptoIntn(t, func(n int) (int, error) {
		calls++
		if calls == 1 {
			return 1, nil // scheme 1
		}
		return 0, errors.New("injected cryptoIntn error")
	})
	if _, err := randomRoomName(); err == nil {
		t.Fatal("expected error from scheme 1")
	}
}

func TestRandomRoomNameSchemeTwoCamelWordsError(t *testing.T) {
	calls := 0
	setCryptoIntn(t, func(n int) (int, error) {
		calls++
		if calls == 1 {
			return 2, nil // scheme 2
		}
		return 0, errors.New("injected cryptoIntn error")
	})
	if _, err := randomRoomName(); err == nil {
		t.Fatal("expected error from scheme 2 camelWords")
	}
}

func TestRandomRoomNameSchemeTwoDigitsError(t *testing.T) {
	calls := 0
	setCryptoIntn(t, func(n int) (int, error) {
		calls++
		switch calls {
		case 1:
			return 2, nil // scheme 2
		case 2, 3:
			return 0, nil // camelWords picks two words
		default:
			return 0, errors.New("injected cryptoIntn error")
		}
	})
	if _, err := randomRoomName(); err == nil {
		t.Fatal("expected error from scheme 2 randomDigits")
	}
}

func TestPickWordError(t *testing.T) {
	setCryptoIntn(t, func(int) (int, error) {
		return 0, errors.New("injected cryptoIntn error")
	})
	if _, err := pickWord(roomAdjectives); err == nil {
		t.Fatal("expected error from pickWord")
	}
}

func TestCamelWordsError(t *testing.T) {
	setCryptoIntn(t, func(int) (int, error) {
		return 0, errors.New("injected cryptoIntn error")
	})
	if _, err := camelWords(2); err == nil {
		t.Fatal("expected error from camelWords")
	}
}

func TestDashWordsError(t *testing.T) {
	setCryptoIntn(t, func(int) (int, error) {
		return 0, errors.New("injected cryptoIntn error")
	})
	if _, err := dashWords(); err == nil {
		t.Fatal("expected error from dashWords")
	}
}

func TestRandomDigitsCryptoIntnError(t *testing.T) {
	setCryptoIntn(t, func(int) (int, error) {
		return 0, errors.New("injected cryptoIntn error")
	})
	if _, err := randomDigits(); err == nil {
		t.Fatal("expected error from randomDigits")
	}
}

func TestRandomDigitsInnerError(t *testing.T) {
	calls := 0
	setCryptoIntn(t, func(n int) (int, error) {
		calls++
		if calls == 2 {
			return 0, errors.New("injected cryptoIntn error")
		}
		return 0, nil
	})
	if _, err := randomDigits(); err == nil {
		t.Fatal("expected error from randomDigits inner loop")
	}
}

func TestRandomHexNameCryptoIntnError(t *testing.T) {
	setCryptoIntn(t, func(int) (int, error) {
		return 0, errors.New("injected cryptoIntn error")
	})
	if _, err := randomHexName(); err == nil {
		t.Fatal("expected error from randomHexName cryptoIntn")
	}
}

func TestRandomHexNameRandReadError(t *testing.T) {
	setCryptoIntn(t, func(int) (int, error) { return 0, nil })
	setRandRead(t, func([]byte) (int, error) {
		return 0, errors.New("injected rand.Read error")
	})
	if _, err := randomHexName(); err == nil {
		t.Fatal("expected error from randomHexName randRead")
	}
}

func TestRandomAlphaNumCryptoIntnError(t *testing.T) {
	setCryptoIntn(t, func(int) (int, error) {
		return 0, errors.New("injected cryptoIntn error")
	})
	if _, err := randomAlphaNum(); err == nil {
		t.Fatal("expected error from randomAlphaNum cryptoIntn")
	}
}

func TestRandomAlphaNumInnerError(t *testing.T) {
	calls := 0
	setCryptoIntn(t, func(n int) (int, error) {
		calls++
		if calls == 2 {
			return 0, errors.New("injected cryptoIntn error")
		}
		return 0, nil
	})
	if _, err := randomAlphaNum(); err == nil {
		t.Fatal("expected error from randomAlphaNum inner loop")
	}
}
