FROM flynn/cedarish

ADD Gemfile /app/Gemfile
ADD Gemfile.lock /app/Gemfile.lock

RUN apt-get update &&\
    apt-get install -y nodejs &&\
    gem install bundler --no-rdoc --no-ri &&\
    cd /app &&\
    bundle install --deployment
