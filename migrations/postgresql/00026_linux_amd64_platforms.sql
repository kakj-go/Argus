-- +goose Up

-- Collector 产物支持 Linux 双架构:放开 Profile 平台矩阵与
-- Collector 实例 platform 的 CHECK 约束,纳入 linux_amd64。
ALTER TABLE collection_profiles DROP CONSTRAINT collection_profiles_supported_platforms_check;
ALTER TABLE collection_profiles ADD CONSTRAINT collection_profiles_supported_platforms_check
    CHECK (cardinality(supported_platforms) > 0 AND supported_platforms <@ ARRAY['linux_arm64','linux_amd64','windows_amd64']::text[]);
ALTER TABLE collector_instances DROP CONSTRAINT collector_instances_platform_check;
ALTER TABLE collector_instances ADD CONSTRAINT collector_instances_platform_check
    CHECK (platform IN ('linux_arm64','linux_amd64','windows_amd64'));

-- +goose Down
ALTER TABLE collector_instances DROP CONSTRAINT collector_instances_platform_check;
ALTER TABLE collector_instances ADD CONSTRAINT collector_instances_platform_check
    CHECK (platform IN ('linux_arm64','windows_amd64'));
ALTER TABLE collection_profiles DROP CONSTRAINT collection_profiles_supported_platforms_check;
ALTER TABLE collection_profiles ADD CONSTRAINT collection_profiles_supported_platforms_check
    CHECK (cardinality(supported_platforms) > 0 AND supported_platforms <@ ARRAY['linux_arm64','windows_amd64']::text[]);
