# frozen_string_literal: true

# Companion source for order.rb — same story: Ruby sources alone must never
# trip the structure tier.

class Customer
  attr_reader :name, :email

  def initialize(name, email)
    @name = name
    @email = email
  end

  def notify(message)
    "#{name} <#{email}>: #{message}"
  end
end
