#!/usr/bin/env bash
# Copyright The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail

task_dir=$(mktemp -d)
trap 'rm -rf "$task_dir"' EXIT
project_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

helm template audit "$project_dir/charts" \
    --api-versions monitoring.coreos.com/v1 \
    --set metrics.prometheusRule.enabled=true \
    --set credentials.ibmApiKey=test-placeholder \
    --set credentials.vpcApiKey=test-placeholder \
    --set credentials.region=us-south \
    --show-only templates/prometheusrule.yaml > "$task_dir/rendered.yaml"
sed -n '/^spec:/,$p' "$task_dir/rendered.yaml" | sed '1d;s/^  //' > "$task_dir/prometheusrule.yaml"
cp "$project_dir/test/metrics/prometheus_rules_test.yaml" "$task_dir/tests.yaml"
promtool check rules "$task_dir/prometheusrule.yaml"
promtool test rules "$task_dir/tests.yaml"
