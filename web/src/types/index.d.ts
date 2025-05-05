import { Telegram } from './telegram'

export {}

declare global {
	interface Window {
		Telegram: Telegram
	}
}
