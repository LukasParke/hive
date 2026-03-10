import { createCipheriv, createDecipheriv, randomBytes } from 'node:crypto';

const ALGORITHM = 'aes-256-gcm';
const NONCE_SIZE = 12; // GCM standard nonce size
const TAG_SIZE = 16; // GCM auth tag size

function getEncryptionKey(): Buffer {
	const hexKey = process.env.HIVE_ENCRYPTION_KEY;
	if (hexKey) {
		const buf = Buffer.from(hexKey, 'hex');
		if (buf.length === 32) return buf;
	}
	if (process.env.NODE_ENV === 'production') {
		throw new Error(
			'HIVE_ENCRYPTION_KEY must be set in production (64 hex chars = 32 bytes)'
		);
	}
	return Buffer.alloc(32, 0);
}

let _key: Buffer | undefined;
function key(): Buffer {
	if (!_key) _key = getEncryptionKey();
	return _key;
}

/**
 * Encrypt plaintext using AES-256-GCM. Output format matches the Go
 * `pkg/encryption` package: `nonce (12 bytes) || ciphertext || auth tag (16 bytes)`.
 * Returns Uint8Array for Prisma Bytes compatibility.
 */
export function encrypt(plaintext: string | Uint8Array): Uint8Array<ArrayBuffer> {
	const input =
		typeof plaintext === 'string' ? Buffer.from(plaintext, 'utf-8') : Buffer.from(plaintext);
	const nonce = randomBytes(NONCE_SIZE);
	const cipher = createCipheriv(ALGORITHM, key(), nonce);
	const encrypted = Buffer.concat([cipher.update(input), cipher.final()]);
	const tag = cipher.getAuthTag();
	const result = Buffer.concat([nonce, encrypted, tag]);
	const ab = new ArrayBuffer(result.length);
	const copy = new Uint8Array(ab);
	copy.set(result);
	return copy;
}

/**
 * Decrypt ciphertext produced by either this function or the Go Encrypt().
 */
export function decrypt(ciphertext: Uint8Array | Buffer): string {
	const buf = Buffer.from(ciphertext);
	if (buf.length < NONCE_SIZE + TAG_SIZE) {
		throw new Error('ciphertext too short');
	}

	const nonce = buf.subarray(0, NONCE_SIZE);
	const tag = buf.subarray(buf.length - TAG_SIZE);
	const encrypted = buf.subarray(NONCE_SIZE, buf.length - TAG_SIZE);

	const decipher = createDecipheriv(ALGORITHM, key(), nonce);
	decipher.setAuthTag(tag);
	const decrypted = Buffer.concat([decipher.update(encrypted), decipher.final()]);
	return decrypted.toString('utf-8');
}
