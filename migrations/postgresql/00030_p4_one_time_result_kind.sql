-- +goose Up

-- PlanV4 一次性结果 v2：公开载荷统一为 command，不再把所有结果伪装成
-- EnrollmentResult。数据库只保存 AEAD 密文与不可逆状态。
ALTER TABLE execution_one_time_results DROP CONSTRAINT IF EXISTS execution_one_time_results_result_kind_check;
ALTER TABLE execution_one_time_results ADD CONSTRAINT execution_one_time_results_result_kind_check
    CHECK (result_kind IN ('connector_install_command','host_install_command','host_uninstall_command'));

-- +goose Down
ALTER TABLE execution_one_time_results DROP CONSTRAINT IF EXISTS execution_one_time_results_result_kind_check;
ALTER TABLE execution_one_time_results ADD CONSTRAINT execution_one_time_results_result_kind_check
    CHECK (result_kind IN ('connector_enrollment'));
