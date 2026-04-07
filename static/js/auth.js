/**
 * 双Token认证管理模块
 * 实现无感刷新机制，优化用户体验
 */

class AuthManager {
    constructor() {
        this.refreshPromise = null;
        this.isRefreshing = false;
        this.refreshSubscribers = [];
    }

    /**
     * 封装fetch请求，自动处理Token刷新
     */
    async fetch(url, options = {}) {
        // 确保options有headers
        if (!options.headers) {
            options.headers = {};
        }

        try {
            const response = await fetch(url, options);

            // 如果返回401，尝试刷新Token
            if (response.status === 401) {
                // 尝试刷新Token
                const refreshed = await this.tryRefreshToken();
                if (refreshed) {
                    // 刷新成功，重试原请求
                    return fetch(url, options);
                } else {
                    // 刷新失败，跳转到登录页
                    this.redirectToLogin();
                    return response;
                }
            }

            return response;
        } catch (error) {
            throw error;
        }
    }

    /**
     * 尝试刷新Token
     */
    async tryRefreshToken() {
        // 如果正在刷新中，等待刷新完成
        if (this.isRefreshing) {
            return new Promise((resolve) => {
                this.refreshSubscribers.push(resolve);
            });
        }

        this.isRefreshing = true;

        try {
            const response = await fetch('/api/refresh', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                credentials: 'same-origin'
            });

            const result = await response.json();

            // 通知所有等待的订阅者
            const success = result.success;
            this.refreshSubscribers.forEach(callback => callback(success));
            this.refreshSubscribers = [];

            return success;
        } catch (error) {
            // 通知所有等待的订阅者刷新失败
            this.refreshSubscribers.forEach(callback => callback(false));
            this.refreshSubscribers = [];
            return false;
        } finally {
            this.isRefreshing = false;
        }
    }

    /**
     * 主动检查Token是否需要刷新（预刷新机制）
     * 可以在页面加载或定时调用
     */
    async proactiveRefresh() {
        // 检查是否有access_token cookie
        const hasAccessToken = document.cookie.includes('access_token=');

        if (!hasAccessToken) {
            // 没有access_token，尝试用refresh_token刷新
            const refreshed = await this.tryRefreshToken();
            if (!refreshed) {
                // 刷新失败，不需要立即跳转，让下次请求时再处理
                console.log('Token刷新失败，将在下次请求时重试');
            }
        }
    }

    /**
     * 登出
     */
    async logout() {
        try {
            await fetch('/api/logout', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                credentials: 'same-origin'
            });
        } catch (error) {
            console.error('登出请求失败:', error);
        } finally {
            // 清除本地状态并跳转
            this.redirectToLogin();
        }
    }

    /**
     * 跳转到登录页
     */
    redirectToLogin() {
        // 清除可能存在的定时器
        if (this.refreshInterval) {
            clearInterval(this.refreshInterval);
            this.refreshInterval = null;
        }

        // 如果不是已经在登录页，则跳转
        if (!window.location.pathname.includes('/login')) {
            window.location.href = '/login';
        }
    }

    /**
     * 启动定时预刷新（可选）
     * 每隔一段时间检查并预刷新Token
     */
    startAutoRefresh(intervalMinutes = 10) {
        // 先清除已有的定时器
        if (this.refreshInterval) {
            clearInterval(this.refreshInterval);
        }

        // 立即执行一次
        this.proactiveRefresh();

        // 设置定时器
        this.refreshInterval = setInterval(() => {
            this.proactiveRefresh();
        }, intervalMinutes * 60 * 1000);
    }

    /**
     * 停止自动刷新
     */
    stopAutoRefresh() {
        if (this.refreshInterval) {
            clearInterval(this.refreshInterval);
            this.refreshInterval = null;
        }
    }
}

// 创建全局实例
const authManager = new AuthManager();

// 页面加载时启动自动刷新
document.addEventListener('DOMContentLoaded', () => {
    // 启动自动刷新，每10分钟检查一次
    authManager.startAutoRefresh(10);
});

// 页面卸载时停止自动刷新
window.addEventListener('beforeunload', () => {
    authManager.stopAutoRefresh();
});

// 导出供其他模块使用
if (typeof module !== 'undefined' && module.exports) {
    module.exports = { AuthManager, authManager };
}
