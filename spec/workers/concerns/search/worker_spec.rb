# frozen_string_literal: true

require 'spec_helper'

RSpec.describe Search::Worker, feature_category: :global_search do
  let(:worker_class) do
    Class.new do
      def self.name
        'Search::Foo::Bar::DummyWorker'
      end

      include ApplicationWorker
      include ::Search::Worker
    end
  end

  let(:worker) { worker_class.new }

  it 'sets the feature category to :global_search' do
    expect(worker_class.get_feature_category).to eq(:global_search)
  end

  it 'sets the concurrency limit to default_concurrency_limit' do
    limit = 55
    expect(Search).to receive(:default_concurrency_limit).and_return(limit)

    expect(Gitlab::SidekiqMiddleware::ConcurrencyLimit::WorkersMap.limit_for(worker: worker_class)&.call).to eq(limit)
  end
end
