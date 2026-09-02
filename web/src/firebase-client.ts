import { FirebaseError, initializeApp, type FirebaseApp } from 'firebase/app';
import { GoogleAuthProvider, getAuth, signInWithPopup, signOut, type Auth } from 'firebase/auth';

export interface FirebaseClientConfig {
  apiKey: string;
  authDomain: string;
  projectId: string;
  appId: string;
}

let firebaseApp: FirebaseApp | undefined;
let firebaseAuth: Auth | undefined;

export async function requestGoogleIDToken(config: FirebaseClientConfig): Promise<string> {
  if (!firebaseApp || !firebaseAuth) {
    firebaseApp = initializeApp(config);
    firebaseAuth = getAuth(firebaseApp);
  }
  const provider = new GoogleAuthProvider();
  provider.setCustomParameters({ prompt: 'select_account' });
  try {
    const result = await signInWithPopup(firebaseAuth, provider);
    return await result.user.getIdToken(true);
  } catch (cause) {
    if (
      cause instanceof FirebaseError &&
      (cause.code === 'auth/popup-closed-by-user' || cause.code === 'auth/cancelled-popup-request')
    ) {
      throw new Error('Google sign-in was cancelled.', { cause });
    }
    throw cause;
  }
}

export async function signOutFromGoogle(): Promise<void> {
  if (firebaseAuth) {
    await signOut(firebaseAuth);
  }
}
