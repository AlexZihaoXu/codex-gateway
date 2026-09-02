import { beforeEach, describe, expect, it, vi } from 'vitest';

const FirebaseErrorMock = vi.hoisted(
  () =>
    class FirebaseError extends Error {
      constructor(public code: string) {
        super(code);
      }
    },
);

const firebase = vi.hoisted(() => ({
  getAuth: vi.fn(),
  getIdToken: vi.fn(),
  initializeApp: vi.fn(),
  provider: { setCustomParameters: vi.fn() },
  signInWithPopup: vi.fn(),
  signOut: vi.fn(),
}));

vi.mock('firebase/app', () => ({
  FirebaseError: FirebaseErrorMock,
  initializeApp: firebase.initializeApp,
}));
vi.mock('firebase/auth', () => ({
  GoogleAuthProvider: function GoogleAuthProvider() {
    return firebase.provider;
  },
  getAuth: firebase.getAuth,
  signInWithPopup: firebase.signInWithPopup,
  signOut: firebase.signOut,
}));

describe('Firebase browser authentication', () => {
  beforeEach(() => {
    vi.resetModules();
    vi.clearAllMocks();
  });

  it('opens the Google account chooser and returns a fresh Firebase ID token', async () => {
    const app = { name: 'test-app' };
    const auth = { name: 'test-auth' };
    const getIdToken = vi.fn().mockResolvedValue('fresh-id-token');
    firebase.initializeApp.mockReturnValue(app);
    firebase.getAuth.mockReturnValue(auth);
    firebase.signInWithPopup.mockResolvedValue({ user: { getIdToken } });
    const { requestGoogleIDToken, signOutFromGoogle } = await import('./firebase-client');

    await expect(
      requestGoogleIDToken({
        apiKey: 'test-api-key',
        appId: 'test-app-id',
        authDomain: 'test.firebaseapp.com',
        projectId: 'test-project',
      }),
    ).resolves.toBe('fresh-id-token');

    expect(firebase.initializeApp).toHaveBeenCalledWith({
      apiKey: 'test-api-key',
      appId: 'test-app-id',
      authDomain: 'test.firebaseapp.com',
      projectId: 'test-project',
    });
    expect(firebase.provider.setCustomParameters).toHaveBeenCalledWith({
      prompt: 'select_account',
    });
    expect(firebase.signInWithPopup).toHaveBeenCalledWith(auth, firebase.provider);
    expect(getIdToken).toHaveBeenCalledWith(true);

    firebase.signOut.mockResolvedValue(undefined);
    await signOutFromGoogle();
    expect(firebase.signOut).toHaveBeenCalledWith(auth);
  });

  it('turns a closed popup into a concise error and can sign out before initialization', async () => {
    const { requestGoogleIDToken, signOutFromGoogle } = await import('./firebase-client');
    await signOutFromGoogle();
    expect(firebase.signOut).not.toHaveBeenCalled();

    firebase.initializeApp.mockReturnValue({ name: 'test-app' });
    firebase.getAuth.mockReturnValue({ name: 'test-auth' });
    firebase.signInWithPopup.mockRejectedValue(new FirebaseErrorMock('auth/popup-closed-by-user'));

    await expect(
      requestGoogleIDToken({
        apiKey: 'test-api-key',
        appId: 'test-app-id',
        authDomain: 'test.firebaseapp.com',
        projectId: 'test-project',
      }),
    ).rejects.toThrow('Google sign-in was cancelled.');
  });

  it('preserves unexpected Firebase failures', async () => {
    const failure = new Error('network unavailable');
    firebase.initializeApp.mockReturnValue({ name: 'test-app' });
    firebase.getAuth.mockReturnValue({ name: 'test-auth' });
    firebase.signInWithPopup.mockRejectedValue(failure);
    const { requestGoogleIDToken } = await import('./firebase-client');

    await expect(
      requestGoogleIDToken({
        apiKey: 'test-api-key',
        appId: 'test-app-id',
        authDomain: 'test.firebaseapp.com',
        projectId: 'test-project',
      }),
    ).rejects.toBe(failure);
  });
});
