# frozen_string_literal: true

class InsertForgottenActiveVersionedPagesDeploymentsLimitByNamespace < Gitlab::Database::Migration[2.2]
  restrict_gitlab_migration gitlab_schema: :gitlab_main
  milestone '17.9'

  def up
    create_or_update_plan_limit('active_versioned_pages_deployments_limit_by_namespace',
      'ultimate_trial_paid_customer', 500)
    create_or_update_plan_limit('active_versioned_pages_deployments_limit_by_namespace',
      'opensource', 500)
  end

  def down
    create_or_update_plan_limit('active_versioned_pages_deployments_limit_by_namespace',
      'ultimate_trial_paid_customer', 0)
    create_or_update_plan_limit('active_versioned_pages_deployments_limit_by_namespace',
      'opensource', 0)
  end
end
