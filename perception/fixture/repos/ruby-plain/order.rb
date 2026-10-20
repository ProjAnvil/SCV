# frozen_string_literal: true

# A tiny plain-Ruby class hierarchy. Ruby is NOT one of the six structure
# languages (java/python/javascript/typescript/go/rust), so this file exists
# purely to be ignored by the detect gate.

class Order
  attr_reader :items, :customer

  def initialize(customer, items = [])
    @customer = customer
    @items = items
  end

  def total
    items.sum(&:price)
  end

  def add(item)
    items << item
    self
  end
end
