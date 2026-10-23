import axios, { AxiosInstance, AxiosError } from 'axios';

export class ApiError extends Error {
  constructor(public status: number, message: string, public body?: unknown) {
    super(message);
    this.name = 'ApiError';
  }
}

export function createApiClient(baseURL: string = '/api/v1'): AxiosInstance {
  const instance = axios.create({
    baseURL,
    withCredentials: true,
    headers: {
      'Content-Type': 'application/json',
    },
  });

  instance.interceptors.response.use(
    (response) => response,
    (error: AxiosError<{ error?: string; message?: string }>) => {
      if (error.response) {
        const status = error.response.status;
        const data = error.response.data;
        const msg = data?.error || data?.message || error.message || `Request failed with status ${status}`;
        return Promise.reject(new ApiError(status, msg, data));
      }
      return Promise.reject(new ApiError(0, error.message || 'Network error'));
    }
  );

  return instance;
}

export const axiosClient = createApiClient();
