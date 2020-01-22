# frozen_string_literal: true

shared_examples 'error tracking index page' do
  it 'renders the error index page' do
    within('div.js-title-container') do
      expect(page).to have_content(project.namespace.name)
      expect(page).to have_content(project.name)
    end

    within('div.error-list') do
      expect(page).to have_content('Error')
      expect(page).to have_content('Events')
      expect(page).to have_content('Users')
      expect(page).to have_content('Last Seen')
    end
  end

  it 'renders the error index data' do
    Timecop.freeze(2020, 01, 01, 12, 0, 0) do
      within('div.error-list') do
        expect(page).to have_content(issues_response[0]['title'])
        expect(page).to have_content(issues_response[0]['count'].to_s)
        expect(page).to have_content(issues_response[0]['last_seen'])
        expect(page).to have_content('1 year ago')
      end
    end
  end

  context 'when error is clicked' do
    before do
      click_on issues_response[0]['title']
    end

    it 'loads the error page' do
      expect(page).to have_content('Error details')
    end
  end
end

shared_examples 'expanded stack trace context' do |selected_line: nil, expected_line: 1|
  it 'expands the stack trace context' do
    within('div.stacktrace') do
      find("div.file-holder:nth-child(#{selected_line}) svg.ic-chevron-right").click if selected_line

      expanded_line = find("div.file-holder:nth-child(#{expected_line})")
      expect(expanded_line).to have_css('svg.ic-chevron-down')

      event_response['entries'][0]['data']['values'][0]['stacktrace']['frames'][-expected_line]['context'].each do |context|
        expect(page).to have_content(context[0])
      end
    end
  end
end

shared_examples 'error tracking show page' do
  it 'renders the error details' do
    release_short_version = issue_response['firstRelease']['shortVersion']

    Timecop.freeze(2020, 01, 01, 12, 0, 0) do
      expect(page).to have_content('1 month ago by raven.scripts.runner in main')
      expect(page).to have_content(issue_response['metadata']['title'])
      expect(page).to have_content('level: error')
      expect(page).to have_content('Error details')
      expect(page).to have_content('GitLab Issue: https://gitlab.com/gitlab-org/gitlab/issues/1')
      expect(page).to have_content("Sentry event: https://sentrytest.gitlab.com/sentry-org/sentry-project/issues/#{issue_id}")
      expect(page).to have_content("First seen: 1 year ago (2018-11-06 9:19:55PM UTC) Release: #{release_short_version}")
      expect(page).to have_content('Events: 1')
      expect(page).to have_content('Users: 0')
    end
  end

  it 'renders the stack trace heading' do
    expect(page).to have_content('Stack trace')
  end

  it 'renders the stack trace' do
    event_response['entries'][0]['data']['values'][0]['stacktrace']['frames'].each do |frame|
      expect(frame['filename']).not_to be_nil
      expect(page).to have_content(frame['filename'])
    end
  end

  # The first line is expanded by default if no line is selected
  it_behaves_like 'expanded stack trace context', selected_line: nil, expected_line: 1
  it_behaves_like 'expanded stack trace context', selected_line: 8, expected_line: 8
end
