FROM flynn/cedarish

ADD Gemfile /app/Gemfile
ADD Gemfile.lock /app/Gemfile.lock

# Install NodeSource repo and nodejs, install then run bundler
RUN curl -sL https://deb.nodesource.com/setup | bash - &&\
    apt-get update &&\
    apt-get install -y nodejs build-essential &&\
    gem install bundler --no-rdoc --no-ri &&\
    cd /app &&\
    bundle install --deployment &&\
    chmod -R o+rw /app
