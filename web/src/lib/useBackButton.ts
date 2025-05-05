export function useBackButton() {
	return {
		setVisible: () => {
			window.Telegram.WebApp.BackButton.show()
		},
		hide() {
			window.Telegram.WebApp.BackButton.isVisible = false
		},
		onClick: (callback: () => void) => {
			window.Telegram.WebApp.BackButton.onClick(callback)
		},
		offClick: (callback: () => void) => {
			window.Telegram.WebApp.BackButton.offClick(callback)
		},
	}
}
