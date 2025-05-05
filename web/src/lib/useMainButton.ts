export function useMainButton() {
	return {
		setVisible: (text: string) => {
			window.Telegram.WebApp.MainButton.setParams({
				is_visible: true,
				text_color: '#FFFFFF',
				color: '#3F8AF7',
				text,
			})
		},
		hide: () => {
			window.Telegram.WebApp.MainButton.isVisible = false
		},
		enable: (text?: string) => {
			return window.Telegram.WebApp.MainButton.setParams({
				is_active: true,
				is_visible: true,
				text_color: window.Telegram.WebApp.themeParams.button_text_color,
				color: window.Telegram.WebApp.themeParams.button_color,
				text,
			})
		},
		disable: (text?: string) => {
			return window.Telegram.WebApp.MainButton.setParams({
				is_active: false,
				color:
					window.Telegram.WebApp.colorScheme === 'dark' ? '#3C3C3E' : '#F7F7F7',
				text_color:
					window.Telegram.WebApp.colorScheme === 'dark' ? '#FFFFFF' : '#3C3C3E',
				is_visible: true,
				text,
			})
		},
		setParams: (params: {
			text?: string
			isVisible?: boolean
			color?: string
			textColor?: string
			isEnabled?: boolean
		}) => {
			return window.Telegram.WebApp.MainButton.setParams({
				is_visible: params.isVisible,
				text: params.text,
				color: params.color,
				text_color: params.textColor,
				is_active: params.isEnabled,
			})
		},
		onClick: (callback: () => void) => {
			window.Telegram.WebApp.MainButton.onClick(callback)
		},
		offClick: (callback: () => void) => {
			window.Telegram.WebApp.MainButton.offClick(callback)
		},
		showProgress: (leaveActive = false) => {
			window.Telegram.WebApp.MainButton.showProgress(leaveActive)
		},
		hideProgress: () => {
			window.Telegram.WebApp.MainButton.hideProgress()
		},
	}
}
