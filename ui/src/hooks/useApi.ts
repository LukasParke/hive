import { useCallback, useState } from "react";

interface UseApiResult<T> {
  data: T | null;
  loading: boolean;
  error: string | null;
  execute: (...args: unknown[]) => Promise<T | undefined>;
}

export function useApi<T>(fn: (...args: unknown[]) => Promise<T>): UseApiResult<T> {
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const execute = useCallback(
    async (...args: unknown[]) => {
      setLoading(true);
      setError(null);
      try {
        const result = await fn(...args);
        setData(result);
        return result;
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        setError(msg);
        throw err;
      } finally {
        setLoading(false);
      }
    },
    [fn],
  );

  return { data, loading, error, execute };
}
