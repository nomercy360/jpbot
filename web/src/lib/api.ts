import { store } from '~/store'

export const API_BASE_URL = import.meta.env.VITE_API_BASE_URL as string

export async function apiRequest(endpoint: string, options: RequestInit = {}) {
	try {
		const response = await fetch(`${API_BASE_URL}/v1${endpoint}`, {
			...options,
			headers: {
				'Content-Type': 'application/json',
				Authorization: `Bearer ${store.token}`,
				...(options.headers || {}),
			},
		})

		let data
		try {
			data = await response.json()
		} catch {
			return { error: 'Failed to get response from server', data: null }
		}

		if (!response.ok) {
			const errorMessage =
				Array.isArray(data?.error)
					? data.error.join('\n')
					: typeof data?.error === 'string'
						? data.error
						: 'An error occurred'

			return { error: errorMessage, data: null }
		}

		return { data, error: null }
	} catch (error) {
		const errorMessage = error instanceof Error ? error.message : 'An unexpected error occurred'
		return { error: errorMessage, data: null }
	}
}

export interface LeaderboardEntry {
	user_id: number;
	username: string;
	first_name: string;
	last_name: string;
	avatar_url: string;
	level: string;
	score: number;
	rank: number;
}

export interface LeaderboardResponse {
	daily: LeaderboardEntry[];
	weekly: LeaderboardEntry[];
	monthly: LeaderboardEntry[];
}

export async function getLeaderboard(limit: number = 100) {
	return apiRequest(`/leaderboard?limit=${limit}`) as Promise<{ data: LeaderboardResponse; error: string | null }>;
}
