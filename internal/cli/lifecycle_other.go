//go:build !darwin && !windows && !linux

package cli

// 其余平台暂不装配生命周期依赖；
// 生命周期命令统一走引导报告（exit 3）。
func defaultLifecycleDependencies() (*LifecycleDependencies, error) {
	return nil, nil
}
