# frozen_string_literal: true

require 'yaml'

module Gitlab
  module SidekiqConfig
    FOSS_QUEUE_CONFIG_PATH = 'app/workers/all_queues.yml'
    EE_QUEUE_CONFIG_PATH = 'ee/app/workers/all_queues.yml'

    QUEUE_CONFIG_PATHS = [
      FOSS_QUEUE_CONFIG_PATH,
      (EE_QUEUE_CONFIG_PATH if Gitlab.ee?)
    ].compact.freeze

    # For queues that don't have explicit workers - default and mailers
    DummyWorker = Struct.new(:queue)

    DEFAULT_WORKERS = [
      Gitlab::SidekiqConfig::Worker.new(DummyWorker.new('default'), ee: false),
      Gitlab::SidekiqConfig::Worker.new(DummyWorker.new('mailers'), ee: false)
    ].freeze

    class << self
      include Gitlab::SidekiqConfig::CliMethods

      def redis_queues
        # Not memoized, because this can change during the life of the application
        Sidekiq::Queue.all.map(&:name)
      end

      def config_queues
        @config_queues ||= begin
          config = YAML.load_file(Rails.root.join('config/sidekiq_queues.yml'))
          config[:queues].map(&:first)
        end
      end

      def cron_workers
        @cron_workers ||= Settings.cron_jobs.map { |job_name, options| options['job_class'].constantize }
      end

      def workers
        @workers ||= begin
          result = []
          result.concat(DEFAULT_WORKERS)
          result.concat(find_workers(Rails.root.join('app', 'workers'), ee: false))

          if Gitlab.ee?
            result.concat(find_workers(Rails.root.join('ee', 'app', 'workers'), ee: true))
          end

          result
        end
      end

      def workers_for_all_queues_yml
        workers.partition(&:ee?).reverse.map(&:sort)
      end

      def all_queues_yml_outdated?
        foss_workers, ee_workers = workers_for_all_queues_yml

        return true if foss_workers != YAML.safe_load(File.read(FOSS_QUEUE_CONFIG_PATH))

        Gitlab.ee? && ee_workers != YAML.safe_load(File.read(EE_QUEUE_CONFIG_PATH))
      end

      private

      def find_workers(root, ee:)
        concerns = root.join('concerns').to_s

        Dir[root.join('**', '*.rb')]
          .reject { |path| path.start_with?(concerns) }
          .map { |path| worker_from_path(path, root) }
          .select { |worker| worker < Sidekiq::Worker }
          .map { |worker| Gitlab::SidekiqConfig::Worker.new(worker, ee: ee) }
      end

      def worker_from_path(path, root)
        ns = Pathname.new(path).relative_path_from(root).to_s.gsub('.rb', '')

        ns.camelize.constantize
      end
    end
  end
end
